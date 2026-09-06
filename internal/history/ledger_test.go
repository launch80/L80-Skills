package history

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppendAndReadNewestFirst(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if got, err := Read(); err != nil || len(got) != 0 {
		t.Fatalf("empty history should be empty, got %v %v", got, err)
	}
	for i, g := range []string{"a", "b", "c"} {
		if err := Append(Entry{GUID: g, URL: "https://x/a/" + g, CreatedAt: "2026-09-0" + string(rune('1'+i)) + "T00:00:00Z"}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Read()
	if err != nil || len(got) != 3 || got[0].GUID != "c" || got[2].GUID != "a" {
		t.Fatalf("got %+v err=%v", got, err)
	}
	info, _ := os.Stat(Path())
	if info.Mode().Perm() != 0o600 {
		t.Errorf("ledger perms = %o, want 600", info.Mode().Perm())
	}
	// A corrupt line is skipped, not fatal.
	f, _ := os.OpenFile(Path(), os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString("not json\n")
	f.Close()
	if got, err := Read(); err != nil || len(got) != 3 {
		t.Fatalf("corrupt line should be skipped: %v %v", len(got), err)
	}
	if filepath.Base(Path()) != "history.jsonl" {
		t.Errorf("path = %s", Path())
	}
}
