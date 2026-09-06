package betterbench

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// RawTemplateID is the server template that stores a results.json as written.
const RawTemplateID = "bench.betterbench.v1"

// RawMaxPayloadBytes mirrors the server's limit for RawTemplateID.
const RawMaxPayloadBytes = 2 * 1024 * 1024

// UpstreamRepo is where BetterBench lives; used to bootstrap it through uvx
// when it is not installed.
const UpstreamRepo = "git+https://github.com/GGZ14/BetterBench"

// Runner is a resolved way to invoke the betterbench CLI.
type Runner struct {
	// Argv prefix, e.g. ["betterbench"] or ["uvx", "--from", <repo>, "betterbench"].
	Argv []string
	// How it was found, for the doctor line and error messages.
	Source string
}

// lookPath and lookPathUvx are swapped in tests.
var lookPath = exec.LookPath

// ErrNoRunner means neither betterbench nor uvx is available.
var ErrNoRunner = errors.New("betterbench is not installed and uvx is not available to bootstrap it")

// ResolveRunner finds how to run BetterBench, in this order:
//
//  1. L80_BETTERBENCH, if set: a path to the betterbench executable (or any
//     command line; it is split on spaces). Lets a checkout's .venv be used.
//  2. `betterbench` on PATH.
//  3. `uvx --from git+https://github.com/GGZ14/BetterBench betterbench`, when
//     uvx is on PATH. uv caches the build, so this is a one-time download.
func ResolveRunner() (Runner, error) {
	if v := strings.TrimSpace(os.Getenv("L80_BETTERBENCH")); v != "" {
		return Runner{Argv: strings.Fields(v), Source: "L80_BETTERBENCH"}, nil
	}
	if p, err := lookPath("betterbench"); err == nil {
		return Runner{Argv: []string{p}, Source: "PATH"}, nil
	}
	if p, err := lookPath("uvx"); err == nil {
		return Runner{Argv: []string{p, "--from", UpstreamRepo, "betterbench"}, Source: "uvx (bootstrapped from GitHub)"}, nil
	}
	return Runner{}, ErrNoRunner
}

// RunOptions are the parts of `betterbench run` this command exposes by name.
// Anything else goes through Extra verbatim.
type RunOptions struct {
	Endpoint                 string
	Model                    string
	Out                      string
	Quick                    bool
	Passes                   int
	Warmup                   int
	Notes                    []string // KEY=VALUE, as --note takes them
	Phases                   []string // "decode", "prefill", "concurrency": each becomes --<phase>
	NoPrefill, NoConcurrency bool
	Extra                    []string // passed through after the named flags
}

// Args builds the argv for `betterbench run`.
func (o RunOptions) Args() []string {
	args := []string{"run", "--endpoint", o.Endpoint, "--model", o.Model, "--out", o.Out}
	if o.Quick {
		args = append(args, "--quick")
	}
	if o.Passes > 0 {
		args = append(args, "--passes", fmt.Sprint(o.Passes))
	}
	if o.Warmup >= 0 && o.warmupSet() {
		args = append(args, "--warmup", fmt.Sprint(o.Warmup))
	}
	for _, n := range o.Notes {
		args = append(args, "--note", n)
	}
	for _, p := range o.Phases {
		args = append(args, "--"+p)
	}
	if o.NoPrefill {
		args = append(args, "--no-prefill")
	}
	if o.NoConcurrency {
		args = append(args, "--no-concurrency")
	}
	return append(args, o.Extra...)
}

// warmupSet distinguishes "--warmup 0" from "not given". Callers set Warmup to
// -1 when the flag was absent.
func (o RunOptions) warmupSet() bool { return o.Warmup >= 0 }

// Execute runs `betterbench run`, streaming its output to out/errOut, and
// returns the results file it wrote.
func Execute(ctx context.Context, r Runner, o RunOptions, out, errOut io.Writer) ([]byte, error) {
	argv := append(append([]string{}, r.Argv...), o.Args()...)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdout = out
	cmd.Stderr = errOut
	cmd.Stdin = nil
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, fmt.Errorf("betterbench exited with status %d", exit.ExitCode())
		}
		return nil, fmt.Errorf("could not start %s: %w", argv[0], err)
	}
	data, err := os.ReadFile(o.Out)
	if err != nil {
		return nil, fmt.Errorf("betterbench finished but %s was not written: %w", o.Out, err)
	}
	return data, nil
}

// Prose is the publisher-supplied text the raw template accepts under "l80".
type Prose struct {
	Title    string    `json:"title,omitempty"`
	Summary  string    `json:"summary,omitempty"`
	Sections []Section `json:"sections,omitempty"`
}

// strippedEnvKeys never leave the machine: the harness records the LAN URL it
// hit and the hostname it ran on. The server's schema refuses both, so a file
// that skipped this step fails there with a pointer rather than publishing.
var strippedEnvKeys = []string{"endpoint", "host"}

// PrepareRaw turns a results.json into the body to publish under
// RawTemplateID: it removes env.endpoint and env.host, sets $template, and
// attaches prose under "l80" if any was given. Values are copied as raw bytes,
// so no number is re-encoded.
func PrepareRaw(data []byte, prose Prose) ([]byte, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSyntax, err)
	}
	if _, ok := top["$template"]; ok {
		if s, _ := templateOf(top); s != RawTemplateID {
			return nil, ErrAlreadyPayload
		}
	}
	envRaw, ok := top["env"]
	if !ok {
		return nil, fmt.Errorf("%w: no env field", ErrNotBetterBench)
	}
	var env map[string]json.RawMessage
	if err := json.Unmarshal(envRaw, &env); err != nil {
		return nil, fmt.Errorf("%w: env is not an object", ErrNotBetterBench)
	}
	if _, ok := env["model"]; !ok {
		return nil, fmt.Errorf("%w: env.model is missing", ErrNotBetterBench)
	}
	for _, k := range strippedEnvKeys {
		delete(env, k)
	}
	envOut, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	top["env"] = envOut
	top["$template"] = json.RawMessage(`"` + RawTemplateID + `"`)

	if prose.Title != "" || prose.Summary != "" || len(prose.Sections) > 0 {
		if err := checkLen("--title", prose.Title, maxTitle); err != nil {
			return nil, err
		}
		if err := checkLen("--summary", prose.Summary, maxSummary); err != nil {
			return nil, err
		}
		for i, s := range prose.Sections {
			if s.Heading == "" || s.Body == "" {
				return nil, &InputError{fmt.Sprintf("section %d needs both a heading and a body", i+1)}
			}
			if err := checkLen("section heading", s.Heading, maxHeading); err != nil {
				return nil, err
			}
			if err := checkLen("section body", s.Body, maxBody); err != nil {
				return nil, err
			}
		}
		if len(prose.Sections) > maxSections {
			return nil, &InputError{fmt.Sprintf("at most %d sections", maxSections)}
		}
		l80, err := json.Marshal(prose)
		if err != nil {
			return nil, err
		}
		top["l80"] = l80
	}

	out, err := json.Marshal(top)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func templateOf(top map[string]json.RawMessage) (string, bool) {
	raw, ok := top["$template"]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// StripsIdentity reports whether a prepared body still carries the fields
// PrepareRaw removes. Used by tests and by publish's belt-and-braces check.
func StripsIdentity(body []byte) bool {
	var top struct {
		Env map[string]json.RawMessage `json:"env"`
	}
	if err := json.Unmarshal(body, &top); err != nil {
		return false
	}
	for _, k := range strippedEnvKeys {
		if _, ok := top.Env[k]; ok {
			return false
		}
	}
	return true
}
