package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/launch80/L80-Skills/internal/api"
	"github.com/launch80/L80-Skills/internal/config"
	"github.com/launch80/L80-Skills/internal/output"
)

// resolveTemplate reconciles the --template flag with the file's own $template.
//
// The two can disagree, and silently picking a winner would publish something
// the caller did not ask for, so:
//
//	flag only             -> injected into the payload
//	field only            -> used as-is (the original behaviour)
//	both, and they match  -> fine; the flag is documenting intent
//	both, and they differ -> refuse, naming each source
//	neither               -> refuse, naming both ways to set it
//
// Returns the resolved id and the body to send, which is the caller's original
// bytes untouched unless an injection was actually required.
func resolveTemplate(data []byte, flagValue, path string) (string, []byte, *api.Error) {
	// json.RawMessage, not `any`: decoding into `any` turns every number into a
	// float64, and re-encoding would then rewrite the caller's values — a large
	// integer comes back in exponent form, and precision past float64 is gone
	// for good. RawMessage hands back each value's original bytes, so injecting
	// a key cannot alter any value the caller wrote.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return "", nil, api.Newf("E_JSON_INVALID",
			"Fix the JSON syntax and retry.",
			"%s is not valid JSON: %v", path, err)
	}

	fromFile := ""
	if raw, ok := fields["$template"]; ok {
		// A non-string $template stays "" here. The server's schema is what
		// rejects it, and it does so with a proper JSON Pointer.
		_ = json.Unmarshal(raw, &fromFile)
	}

	switch {
	case flagValue == "" && fromFile == "":
		return "", nil, api.Newf("E_TEMPLATE_MISSING",
			"Pass --template <id>, or add a \"$template\" field. Run `L80 templates list` to see valid ids.",
			"%s has no $template field and no --template flag was given", path)

	case flagValue == "":
		return fromFile, data, nil

	case fromFile == "":
		quoted, err := json.Marshal(flagValue)
		if err != nil {
			return "", nil, api.Newf("E_INTERNAL", "Retry once.",
				"could not encode template id: %v", err)
		}
		fields["$template"] = quoted
		merged, err := json.Marshal(fields)
		if err != nil {
			return "", nil, api.Newf("E_INTERNAL", "Retry once.",
				"could not re-encode %s: %v", path, err)
		}
		return flagValue, merged, nil

	case fromFile != flagValue:
		return "", nil, api.Newf("E_TEMPLATE_CONFLICT",
			"Remove one of them, or make them agree, then retry.",
			"--template says %q but %s already declares %q", flagValue, path, fromFile)

	default:
		return flagValue, data, nil
	}
}

func runPublish(e env, args []string) int {
	fs := flag.NewFlagSet("publish", flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	apiBase, asJSON := addCommonFlags(fs)
	dryRun := fs.Bool("dry-run", false, "validate locally and report, without publishing")
	tmpl := fs.String("template", "", "template id, e.g. bench.report.v1")

	positional, parseErr := parseArgs(fs, args)
	if parseErr != nil {
		return api.ExitUsage
	}
	if len(positional) != 1 {
		return fail(e, api.Newf("E_USAGE",
			"Pass exactly one path, e.g. `L80 publish report.json --template bench.report.v1`.",
			"expected one file argument, got %d", len(positional)))
	}

	path := positional[0]
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return fail(e, api.Newf("E_INPUT_INVALID",
			"Check the path. Ask the user where the report file is if it is not obvious.",
			"could not read %s: %v", path, readErr))
	}

	// Parsing here exists to give a fast, precise error and to reconcile the
	// template id. The server revalidates everything; none of it is a security
	// control.
	template, body, tmplErr := resolveTemplate(data, *tmpl, path)
	if tmplErr != nil {
		return fail(e, tmplErr)
	}

	// Checked after resolution, because injecting the id makes the body longer.
	if len(body) > api.MaxPayloadBytes {
		return fail(e, api.Newf("E_PAYLOAD_TOO_LARGE",
			"Shorten the sections and retry.",
			"%s is %d bytes; the limit is %d", path, len(body), api.MaxPayloadBytes))
	}

	if *dryRun {
		if *asJSON {
			return emitJSON(e, map[string]any{
				"dry_run": true, "template": template, "byte_size": len(body), "path": path,
			})
		}
		output.Successf(e.stdout, "dry run: %s would publish %d bytes", path, len(body))
		output.Detailf(e.stdout, "template %s (not sent)", template)
		return api.ExitOK
	}

	cfg := config.Load(*apiBase)
	artifact, apiErr := newClient(cfg).Publish(body)
	if apiErr != nil {
		return fail(e, apiErr)
	}

	if *asJSON {
		return emitJSON(e, artifact)
	}

	// One quotable line the model can hand straight to the user.
	output.Successf(e.stdout, "published: %s", artifact.URL)
	output.Detailf(e.stdout, "template %s v%d · %s · %d bytes",
		artifact.TemplateID, artifact.TemplateVersion, artifact.TrustTier, artifact.ByteSize)
	return api.ExitOK
}

func emitJSON(e env, v any) int {
	enc := json.NewEncoder(e.stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(e.stderr, "error: E_INTERNAL")
		return api.ExitInternal
	}
	return api.ExitOK
}
