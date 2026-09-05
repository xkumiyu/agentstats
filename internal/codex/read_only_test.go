package codex

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDoesNotModifyHistoryFile(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s"}}`+"\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), CacheDir: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.Size() != after.Size() || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("history metadata changed: before=%v after=%v", before, after)
	}
}
