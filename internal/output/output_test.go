package output

import (
	"strings"
	"testing"
)

// The single most important property of this package: a real token must never
// survive intact into anything the CLI prints.
func TestMaskNeverRevealsTheWholeSecret(t *testing.T) {
	token := "L80_example-fake-for-test-only_9f1b2c4e6d0a4f2191b72c8ee9a1b4f0"
	masked := Mask(token)

	if strings.Contains(masked, "9f1b2c4e6d0a4f2191b72c8ee9a1b4f0") {
		t.Fatalf("Mask leaked the secret: %s", masked)
	}
	if masked == token {
		t.Fatal("Mask returned the token unchanged")
	}
	if !strings.Contains(masked, "...") {
		t.Errorf("expected an elision marker, got %s", masked)
	}
}

func TestMaskHandlesEmptyAndShortInput(t *testing.T) {
	if got := Mask(""); got != "(none)" {
		t.Errorf("empty = %q, want (none)", got)
	}
	if got := Mask("abc"); strings.Contains(got, "abc") {
		t.Errorf("short secret leaked: %q", got)
	}
}
