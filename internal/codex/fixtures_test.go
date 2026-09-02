package codex

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFixturesAreReadableAndOnlyMalformedFixtureHasInvalidLines(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 3 {
		t.Fatalf("expected fixture set, got %d", len(files))
	}
	for _, path := range files {
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(file)
		valid, invalid := 0, 0
		for scanner.Scan() {
			if json.Valid(scanner.Bytes()) {
				valid++
			} else {
				invalid++
			}
		}
		if err := scanner.Err(); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if valid == 0 {
			t.Errorf("%s has no valid records", path)
		}
		if filepath.Base(path) == "malformed.jsonl" && invalid != 1 {
			t.Errorf("malformed fixture invalid lines = %d, want 1", invalid)
		}
		if filepath.Base(path) != "malformed.jsonl" && invalid != 0 {
			t.Errorf("%s has %d unexpected invalid lines", path, invalid)
		}
	}
}
