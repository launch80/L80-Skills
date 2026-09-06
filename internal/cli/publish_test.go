package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFieldOnlyIsUsedAsIs(t *testing.T) {
	in := []byte(`{"$template":"bench.report.v1","title":"x"}`)
	got, body, err := resolveTemplate(in, "", "report.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "bench.report.v1" {
		t.Fatalf("template = %q, want bench.report.v1", got)
	}
	// The caller's bytes must go out untouched when nothing needed changing.
	if string(body) != string(in) {
		t.Fatalf("body was rewritten:\n got %s\nwant %s", body, in)
	}
}

func TestFlagOnlyIsInjected(t *testing.T) {
	in := []byte(`{"title":"x"}`)
	got, body, err := resolveTemplate(in, "generic.report.v1", "report.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "generic.report.v1" {
		t.Fatalf("template = %q", got)
	}

	var out map[string]any
	if e := json.Unmarshal(body, &out); e != nil {
		t.Fatalf("injected body is not valid JSON: %v", e)
	}
	if out["$template"] != "generic.report.v1" {
		t.Fatalf("$template = %v, want generic.report.v1", out["$template"])
	}
	if out["title"] != "x" {
		t.Fatalf("injection lost a field: %v", out)
	}
}

func TestMatchingFlagAndFieldIsAccepted(t *testing.T) {
	in := []byte(`{"$template":"bench.report.v1","title":"x"}`)
	got, body, err := resolveTemplate(in, "bench.report.v1", "report.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "bench.report.v1" {
		t.Fatalf("template = %q", got)
	}
	if string(body) != string(in) {
		t.Fatalf("body rewritten when nothing needed changing")
	}
}

func TestConflictIsRefusedAndNamesBothSources(t *testing.T) {
	in := []byte(`{"$template":"generic.report.v1"}`)
	_, _, err := resolveTemplate(in, "bench.report.v1", "report.json")
	if err == nil {
		t.Fatal("expected a conflict error")
	}
	if err.Code != "E_TEMPLATE_CONFLICT" {
		t.Fatalf("code = %s, want E_TEMPLATE_CONFLICT", err.Code)
	}
	// The message has to name both sides, or the caller cannot tell which to fix.
	for _, want := range []string{"bench.report.v1", "generic.report.v1", "report.json"} {
		if !strings.Contains(err.Message, want) {
			t.Errorf("message %q does not mention %q", err.Message, want)
		}
	}
}

func TestNeitherIsRefusedAndOffersBothRoutes(t *testing.T) {
	_, _, err := resolveTemplate([]byte(`{"title":"x"}`), "", "report.json")
	if err == nil {
		t.Fatal("expected an error")
	}
	if err.Code != "E_TEMPLATE_MISSING" {
		t.Fatalf("code = %s, want E_TEMPLATE_MISSING", err.Code)
	}
	if !strings.Contains(err.Remedy, "--template") || !strings.Contains(err.Remedy, "$template") {
		t.Errorf("remedy should offer both routes, got %q", err.Remedy)
	}
}

// Injection re-encodes the object, so it must not disturb any value the caller
// wrote. Decoding through `any` would silently turn these into float64 and hand
// back 1.2345678901234568e+18 — a different number than was measured.
func TestInjectionPreservesNumbersExactly(t *testing.T) {
	in := []byte(`{"big":1234567890123456789,"exact":0.1000000000000000055511151231257827,"neg":-0}`)
	_, body, err := resolveTemplate(in, "generic.report.v1", "report.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		`"big":1234567890123456789`,
		`"exact":0.1000000000000000055511151231257827`,
		`"neg":-0`,
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("number was rewritten; %s missing from %s", want, body)
		}
	}
}

func TestInjectionPreservesNestedStructure(t *testing.T) {
	in := []byte(`{"system":{"model":"Qwen3-8B","notes":[{"label":"quant","value":"mxfp4"}]}}`)
	_, body, err := resolveTemplate(in, "bench.report.v1", "report.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(body), `{"label":"quant","value":"mxfp4"}`) {
		t.Errorf("nested value was rewritten: %s", body)
	}
}

func TestNonObjectPayloadIsRejected(t *testing.T) {
	_, _, err := resolveTemplate([]byte(`[1,2,3]`), "bench.report.v1", "report.json")
	if err == nil || err.Code != "E_JSON_INVALID" {
		t.Fatalf("expected E_JSON_INVALID, got %v", err)
	}
}

// A non-string $template is left for the server's schema, which reports it with
// a JSON Pointer. The flag still applies rather than the CLI guessing.
func TestNonStringFieldFallsThroughToTheFlag(t *testing.T) {
	got, body, err := resolveTemplate([]byte(`{"$template":42}`), "bench.report.v1", "report.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "bench.report.v1" {
		t.Fatalf("template = %q", got)
	}
	if !strings.Contains(string(body), `"$template":"bench.report.v1"`) {
		t.Errorf("flag should have been written in: %s", body)
	}
}

// The body is always sent compact so the 64 KB limit means the same thing
// however the file was indented. Values must still come through byte-for-byte.
func TestPassThroughIsCompactedWithoutRewritingValues(t *testing.T) {
	in := []byte("{\n  \"$template\": \"bench.report.v1\",\n  \"title\": \"x\",\n  \"big\": 1234567890123456789,\n  \"s\": \"a  b\\n\"\n}\n")
	_, body, err := resolveTemplate(in, "", "report.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `{"$template":"bench.report.v1","title":"x","big":1234567890123456789,"s":"a  b\n"}`
	if string(body) != want {
		t.Fatalf("body = %s\nwant %s", body, want)
	}
	// Same when the flag merely agrees with the field.
	_, body2, err := resolveTemplate(in, "bench.report.v1", "report.json")
	if err != nil || string(body2) != want {
		t.Fatalf("matching-flag body = %s err=%v", body2, err)
	}
}

// A raw BetterBench results.json has none of the fields every template requires
// and several the harness writes. Pointing `L80 publish` at it must produce a
// dedicated error, before the size check, whatever the --template flag says.
func TestHarnessDumpIsRefusedBeforeAnythingElse(t *testing.T) {
	dump := []byte(`{"betterbench_version":"0.2.3","corpus_version":"1","env":{"model":"m"},"config":{"runs_per_category":20},"sample_gate":{},"results":{}}`)
	for _, flag := range []string{"", "bench.report.v1"} {
		_, _, err := resolveTemplate(dump, flag, "spec7_quick_0904.json")
		if err == nil || err.Code != "E_INPUT_NOT_TEMPLATE" {
			t.Fatalf("flag=%q: expected E_INPUT_NOT_TEMPLATE, got %v", flag, err)
		}
		if !strings.Contains(err.Message, "spec7_quick_0904.json") || !strings.Contains(err.Remedy, "results.json") {
			t.Errorf("flag=%q: message/remedy should name the file and results.json: %q / %q", flag, err.Message, err.Remedy)
		}
	}
}

// A mapped payload that happens to carry a harness-looking key (e.g. a
// publisher who kept "config") is NOT a dump once title/summary are present;
// the server's schema is what rejects the stray key, with a JSON Pointer.
func TestMappedPayloadWithStrayHarnessKeyIsNotTreatedAsDump(t *testing.T) {
	in := []byte(`{"title":"x","summary":"y","system":{"model":"m"},"config":{}}`)
	got, _, err := resolveTemplate(in, "bench.report.v1", "report.json")
	if err != nil || got != "bench.report.v1" {
		t.Fatalf("got %q err=%v", got, err)
	}
}
