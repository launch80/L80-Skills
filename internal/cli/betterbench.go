package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/launch80/L80-Skills/internal/api"
	"github.com/launch80/L80-Skills/internal/betterbench"
	"github.com/launch80/L80-Skills/internal/output"
)

// multiFlag collects a repeatable string flag.
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ", ") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// defaultPayloadPath is <results>.l80.json beside the input, so the mapped
// file is easy to find and never overwrites the results it came from.
func defaultPayloadPath(results string) string {
	base := strings.TrimSuffix(results, filepath.Ext(results))
	return base + ".l80.json"
}

func runBetterbench(e env, args []string) int {
	fs := flag.NewFlagSet("betterbench", flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	apiBase, asJSON := addCommonFlags(fs)
	out := fs.String("out", "", "where to write the payload (default <results>.l80.json; \"-\" for stdout)")
	title := fs.String("title", "", "page title (default \"<model> on <engine>\")")
	subtitle := fs.String("subtitle", "", "one-line subtitle (default: hardware, passes, sampling)")
	summary := fs.String("summary", "", "summary paragraph (default: generated from the measured figures)")
	engine := fs.String("engine", "", "serving stack, e.g. \"vLLM 0.9.3\" (default: the engine/server/image --note)")
	hardware := fs.String("hardware", "", "the box, e.g. \"RTX 4090 24GB\" (default: from nvidia-smi/rocm-smi in the file)")
	var sections multiFlag
	fs.Var(&sections, "section", "extra prose section as \"Heading=Body\"; repeatable")
	publish := fs.Bool("publish", false, "publish the payload immediately after writing it")

	positional, parseErr := parseArgs(fs, args)
	if parseErr != nil {
		return api.ExitUsage
	}
	if len(positional) != 1 {
		return fail(e, api.Newf("E_USAGE",
			"Pass exactly one BetterBench results file, e.g. `L80 betterbench results.json`.",
			"expected one file argument, got %d", len(positional)))
	}
	path := positional[0]

	data, err := os.ReadFile(path)
	if err != nil {
		return fail(e, api.Newf("E_INPUT_INVALID",
			"Check the path. It should be the results.json that `betterbench run --out` wrote.",
			"could not read %s: %v", path, err))
	}
	results, err := betterbench.Parse(data)
	if err != nil {
		switch {
		case errors.Is(err, betterbench.ErrAlreadyPayload):
			return fail(e, api.Newf("E_INPUT_INVALID",
				fmt.Sprintf("This file is already a template payload. Publish it directly: `L80 publish %s`.", path),
				"%s is %v", path, err))
		case errors.Is(err, betterbench.ErrNotBetterBench):
			return fail(e, api.Newf("E_INPUT_INVALID",
				"Pass the results.json written by `betterbench run --out`.",
				"%s is %v", path, err))
		case errors.Is(err, betterbench.ErrSyntax):
			return fail(e, api.Newf("E_JSON_INVALID", "Fix the JSON syntax and retry.", "%s: %v", path, err))
		default:
			return fail(e, api.Newf("E_INPUT_INVALID",
				"Check that this is an unmodified results.json from a supported BetterBench version (0.2.3 or later).",
				"%s %v", path, err))
		}
	}

	opt := betterbench.Options{Title: *title, Subtitle: *subtitle, Summary: *summary, Engine: *engine, Hardware: *hardware}
	for _, s := range sections {
		heading, body, ok := strings.Cut(s, "=")
		if !ok {
			return fail(e, api.Newf("E_USAGE",
				"Write each section as --section \"Heading=Body text\".",
				"--section %q has no '='", s))
		}
		opt.Sections = append(opt.Sections, betterbench.Section{Heading: strings.TrimSpace(heading), Body: strings.TrimSpace(body)})
	}

	payload, err := betterbench.Build(results, opt)
	if err != nil {
		var ie *betterbench.InputError
		if errors.As(err, &ie) {
			return fail(e, api.Newf("E_INPUT_INVALID", "Shorten that flag's text and retry.", "%s", ie.Msg))
		}
		return fail(e, api.Newf("E_INTERNAL", "Retry once.", "could not build payload: %v", err))
	}

	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fail(e, api.Newf("E_INTERNAL", "Retry once.", "could not encode payload: %v", err))
	}
	body = append(body, '\n')
	compactSize := len(mustCompact(body))
	if compactSize > api.MaxPayloadBytes {
		// Cannot happen for a schema-shaped payload, but say so rather than
		// hand the user a file that publish will refuse.
		return fail(e, api.Newf("E_PAYLOAD_TOO_LARGE",
			"Drop --section text or shorten --summary.",
			"mapped payload is %d bytes; the limit is %d", compactSize, api.MaxPayloadBytes))
	}

	dest := *out
	if dest == "" {
		dest = defaultPayloadPath(path)
	}
	if dest == "-" {
		if *publish {
			return fail(e, api.Newf("E_USAGE", "Write to a file to publish it: drop `--out -`.",
				"--publish cannot be combined with --out -"))
		}
		if _, err := e.stdout.Write(body); err != nil {
			return api.ExitInternal
		}
		return api.ExitOK
	}
	if err := os.WriteFile(dest, body, 0o644); err != nil {
		return fail(e, api.Newf("E_INPUT_INVALID", "Pass --out with a writable path.", "could not write %s: %v", dest, err))
	}

	if *publish {
		pubArgs := []string{dest, "--template", betterbench.TemplateID}
		if *asJSON {
			pubArgs = append(pubArgs, "--json")
		}
		if *apiBase != "" {
			pubArgs = append(pubArgs, "--api-base", *apiBase)
		}
		output.Detailf(e.stderr, "wrote %s (%d bytes); publishing", dest, compactSize)
		return runPublish(e, pubArgs)
	}

	if *asJSON {
		return emitJSON(e, map[string]any{
			"path": dest, "template": betterbench.TemplateID, "byte_size": compactSize,
			"title": payload.Title, "categories": len(payload.Categories),
			"concurrency_levels": len(payload.Concurrency), "prefill_depths": len(payload.Prefill),
			"caveats": payload.Caveats,
		})
	}
	output.Successf(e.stdout, "wrote %s", dest)
	output.Detailf(e.stdout, "%s · %d bytes · %d categories, %d concurrency levels, %d prefill depths",
		betterbench.TemplateID, compactSize, len(payload.Categories), len(payload.Concurrency), len(payload.Prefill))
	output.Detailf(e.stdout, "title: %s", payload.Title)
	for _, c := range payload.Caveats {
		output.Detailf(e.stdout, "caveat: %s", c)
	}
	output.Detailf(e.stdout, "review the file, then: L80 publish %s", dest)
	return api.ExitOK
}

func mustCompact(b []byte) []byte {
	c, err := compact(b)
	if err != nil {
		return b
	}
	return c
}
