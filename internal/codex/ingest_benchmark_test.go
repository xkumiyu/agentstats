package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func BenchmarkLoadCache(b *testing.B) {
	home := b.TempDir()
	path := filepath.Join(home, "sessions", "synthetic.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		b.Fatal(err)
	}
	var contents strings.Builder
	contents.WriteString(`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"synthetic"}}`)
	contents.WriteByte('\n')
	for index := 0; index < 500; index++ {
		when := time.Date(2026, 1, 1, 0, 0, index, 0, time.UTC).Add(time.Duration(index/2) * 24 * time.Hour)
		fmt.Fprintf(&contents, `{"timestamp":%q,"type":"user_message","payload":{"text":"prompt-%d"}}`+"\n", when.Format(time.RFC3339Nano), index)
		fmt.Fprintf(&contents, `{"timestamp":%q,"type":"task_complete","payload":{}}`+"\n", when.Add(time.Second).Format(time.RFC3339Nano))
	}
	if err := os.WriteFile(path, []byte(contents.String()), 0o644); err != nil {
		b.Fatal(err)
	}
	now := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	run := func(cacheDir string, days int, daysSet bool) {
		b.Helper()
		if _, err := Load(home, IngestOptions{Now: now, Days: days, DaysSet: daysSet, CacheDir: cacheDir}); err != nil {
			b.Fatal(err)
		}
	}

	for _, test := range []struct {
		name    string
		days    int
		daysSet bool
	}{
		{name: "all", days: 0},
		{name: "days1", days: 1, daysSet: true},
	} {
		b.Run(test.name+"/cold", func(b *testing.B) {
			b.ReportAllocs()
			root := b.TempDir()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				run(filepath.Join(root, strconv.Itoa(index)), test.days, test.daysSet)
			}
		})
		b.Run(test.name+"/warm", func(b *testing.B) {
			cacheDir := b.TempDir()
			run(cacheDir, test.days, test.daysSet)
			b.ReportAllocs()
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				run(cacheDir, test.days, test.daysSet)
			}
		})
	}
}
