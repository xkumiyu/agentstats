package codex

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzDecodeFileDoesNotPanic(f *testing.F) {
	f.Add([]byte(`{"type":"session_meta","payload":{"id":"s"}}`))
	f.Add([]byte(`not-json`))
	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "fuzz.jsonl")
		if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
		warnings := &WarningCollector{}
		if err := DecodeFile(path, DecodeOptions{MaxLineBytes: 4096, Warnings: warnings}, func(Envelope) {}); err != nil {
			t.Fatal(err)
		}
	})
}
