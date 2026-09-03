package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/xkumiyu/agentstats/internal/usage"
	appversion "github.com/xkumiyu/agentstats/internal/version"
)

func testHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "2026", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s","cli_version":"1"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"task_started","payload":{"turn_id":"t"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"user_message","payload":{"text":"hello"}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"response_item","payload":{"type":"custom_tool_call","name":"exec","call_id":"c","input":"{\"cmd\":\"echo ok\"}"}}`,
		`{"timestamp":"2026-01-01T00:00:04Z","type":"event_msg","payload":{"type":"ItemCompleted","item":{"type":"CommandExecution","id":"i","status":"completed","command":"echo ok"}}}`,
		`{"timestamp":"2026-01-01T00:00:05Z","type":"task_complete","payload":{"turn_id":"t"}}`,
		`{"timestamp":"2026-01-01T00:00:06Z","type":"task_started","payload":{"turn_id":"t2"}}`,
		`{"timestamp":"2026-01-01T00:00:07Z","type":"user_message","payload":{"text":"$report"}}`,
		`{"timestamp":"2026-01-01T00:00:08Z","type":"task_complete","payload":{"turn_id":"t2"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestRunCommandsAndMachineOutput(t *testing.T) {
	home := testHome(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"stats", "--codex-home", home, "--color", "never"}, &stdout, &stderr); code != 0 {
		t.Fatalf("stats exit=%d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"USAGE OVERVIEW", "Agent: Codex", "Sessions", "User Prompts", "Tool Calls", "Skill Uses"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stats missing %q: %s", want, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "AGENTSTATS") || strings.Contains(stdout.String(), " · ") {
		t.Fatalf("stats contains obsolete display: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "\x1b[") {
		t.Fatal("plain report contains ANSI")
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"stats", "--codex-home", home, "--json", "--color", "always"}, &stdout, &stderr); code != 0 {
		t.Fatalf("json exit=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "\x1b[") {
		t.Fatal("JSON contains ANSI")
	}
	var value map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
		t.Fatalf("invalid JSON: %v (%s)", err, stdout.String())
	}
	if value["tool_calls"] != float64(1) {
		t.Fatalf("tool_calls=%v", value["tool_calls"])
	}
	stdout.Reset()
	if code := run([]string{"tools", "--codex-home", home, "--color", "never"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "shell") || !strings.Contains(stdout.String(), "1 tool, 1 call total") {
		t.Fatalf("tools exit=%d output=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := run([]string{"tools", "--codex-home", home, "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("tools JSON exit=%d stderr=%s", code, stderr.String())
	}
	var toolsValue struct {
		Rows []struct {
			Calls int `json:"calls"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &toolsValue); err != nil {
		t.Fatal(err)
	}
	toolTotal := 0
	for _, row := range toolsValue.Rows {
		toolTotal += row.Calls
	}
	stdout.Reset()
	if code := run([]string{"stats", "--codex-home", home, "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("stats JSON exit=%d stderr=%s", code, stderr.String())
	}
	var statsValue struct {
		ToolCalls int `json:"tool_calls"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &statsValue); err != nil {
		t.Fatal(err)
	}
	if toolTotal != statsValue.ToolCalls {
		t.Fatalf("overview/tools mismatch: %d != %d", statsValue.ToolCalls, toolTotal)
	}
	stdout.Reset()
	if code := run([]string{"skills", "--codex-home", home, "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("skills JSON exit=%d stderr=%s", code, stderr.String())
	}
	var skillsValue struct {
		Rows []struct {
			Total int `json:"total"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &skillsValue); err != nil {
		t.Fatal(err)
	}
	skillTotal := 0
	for _, row := range skillsValue.Rows {
		skillTotal += row.Total
	}
	stdout.Reset()
	if code := run([]string{"stats", "--codex-home", home, "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("stats JSON exit=%d stderr=%s", code, stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &statsValue); err != nil {
		t.Fatal(err)
	}
	var statsSkills struct {
		SkillUses int `json:"skill_uses"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &statsSkills); err != nil {
		t.Fatal(err)
	}
	if skillTotal != statsSkills.SkillUses {
		t.Fatalf("overview/skills mismatch: %d != %d", statsSkills.SkillUses, skillTotal)
	}
}

func TestRunVersionFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("version exit=%d stderr=%s", code, stderr.String())
	}
	if got, want := stdout.String(), "agentstats "+appversion.String()+"\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("version wrote stderr: %q", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"stats", "--version"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "use agentstats --version") {
		t.Fatalf("subcommand version exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunHelpIsScopedToCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("root help exit=%d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"Usage:", "agentstats <command> [options]", "stats", "tools", "skills", "--version"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("root help missing %q: %s", want, stdout.String())
		}
	}
	for _, unwanted := range []string{"--days", "--layer", "--strict"} {
		if helpContainsOption(stdout.String(), unwanted) {
			t.Errorf("root help contains command option %q: %s", unwanted, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("root help wrote stderr: %q", stderr.String())
	}

	for _, test := range []struct {
		command string
		want    []string
		omit    []string
	}{
		{command: "stats", want: []string{"Usage: agentstats stats [options]", "--days", "--group-by"}, omit: []string{"--layer", "--strict"}},
		{command: "tools", want: []string{"Usage: agentstats tools [options]", "--days", "--layer"}, omit: []string{"--group-by", "--strict"}},
		{command: "skills", want: []string{"Usage: agentstats skills [options]", "--days", "--group-by", "--strict"}, omit: []string{"--layer"}},
	} {
		stdout.Reset()
		stderr.Reset()
		if code := run([]string{test.command, "--help"}, &stdout, &stderr); code != 0 {
			t.Fatalf("%s help exit=%d stderr=%s", test.command, code, stderr.String())
		}
		for _, want := range test.want {
			if !strings.Contains(stdout.String(), want) {
				t.Errorf("%s help missing %q: %s", test.command, want, stdout.String())
			}
		}
		for _, unwanted := range test.omit {
			if helpContainsOption(stdout.String(), unwanted) {
				t.Errorf("%s help contains unrelated option %q: %s", test.command, unwanted, stdout.String())
			}
		}
		if stderr.Len() != 0 {
			t.Errorf("%s help wrote stderr: %q", test.command, stderr.String())
		}
	}
}

func helpContainsOption(text, option string) bool {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == option {
			return true
		}
	}
	return false
}

func TestRunSkillsSupportsSessionGrouping(t *testing.T) {
	home := t.TempDir()
	writeSession := func(id string, turns int) {
		history := filepath.Join(home, "sessions", id+".jsonl")
		lines := []string{`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"` + id + `"}}`}
		for i := 0; i < turns; i++ {
			turnID := id + "-t" + strconv.Itoa(i+1)
			lines = append(lines,
				`{"timestamp":"2026-01-01T00:00:0`+strconv.Itoa(i+1)+`Z","type":"task_started","payload":{"turn_id":"`+turnID+`"}}`,
				`{"timestamp":"2026-01-01T00:00:0`+strconv.Itoa(i+1)+`Z","type":"user_message","payload":{"text":"$report"}}`,
				`{"timestamp":"2026-01-01T00:00:0`+strconv.Itoa(i+1)+`Z","type":"task_complete","payload":{"turn_id":"`+turnID+`"}}`,
			)
		}
		if err := os.MkdirAll(filepath.Dir(history), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(history, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSession("s1", 2)
	writeSession("s2", 1)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"skills", "--codex-home", home, "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("turn grouping exit=%d stderr=%s", code, stderr.String())
	}
	var turnValue struct {
		GroupBy string `json:"group_by"`
		Rows    []struct {
			Total int `json:"total"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &turnValue); err != nil {
		t.Fatal(err)
	}
	if turnValue.GroupBy != "turn" || len(turnValue.Rows) != 1 || turnValue.Rows[0].Total != 3 {
		t.Fatalf("turn grouping = %#v", turnValue)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"skills", "--codex-home", home, "--json", "--group-by", "session"}, &stdout, &stderr); code != 0 {
		t.Fatalf("session grouping exit=%d stderr=%s", code, stderr.String())
	}
	var sessionValue struct {
		GroupBy string `json:"group_by"`
		Rows    []struct {
			Total int `json:"total"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &sessionValue); err != nil {
		t.Fatal(err)
	}
	if sessionValue.GroupBy != "session" || len(sessionValue.Rows) != 1 || sessionValue.Rows[0].Total != 2 {
		t.Fatalf("session grouping = %#v", sessionValue)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"stats", "--codex-home", home, "--json", "--group-by", "session"}, &stdout, &stderr); code != 0 {
		t.Fatalf("stats session grouping exit=%d stderr=%s", code, stderr.String())
	}
	var statsValue struct {
		GroupBy   string `json:"group_by"`
		SkillUses int    `json:"skill_uses"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &statsValue); err != nil {
		t.Fatal(err)
	}
	if statsValue.GroupBy != "session" || statsValue.SkillUses != 2 {
		t.Fatalf("stats session grouping = %#v", statsValue)
	}
}

func TestRunRejectsInvalidArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"unknown"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("unknown command exit=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"stats", "--codex-home", testHome(t), "--csv"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "flag provided but not defined") {
		t.Fatalf("removed csv option exit=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"stats", "--days", "0"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "days") {
		t.Fatalf("days validation exit=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"stats", "extra"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "unexpected argument") {
		t.Fatalf("extra argument exit=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"skills", "--group-by", "event"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "invalid --group-by") {
		t.Fatalf("invalid group-by exit=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"tools", "--group-by", "session"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "only valid for stats or skills") {
		t.Fatalf("tools group-by exit=%d stderr=%s", code, stderr.String())
	}
}

func TestRunKeepsWarningsOffMachineReadableStdout(t *testing.T) {
	home := testHome(t)
	path := filepath.Join(home, "sessions", "2026", "one.jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("not-json\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"stats", "--codex-home", home, "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var value map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
		t.Fatalf("stdout is not JSON: %v (%s)", err, stdout.String())
	}
	if !strings.Contains(stderr.String(), "malformed_json") {
		t.Fatalf("warning missing from stderr: %s", stderr.String())
	}
}

func TestWriteWarningsAggregatesByReasonAndType(t *testing.T) {
	warnings := []usage.Warning{
		{Reason: "unknown_type", Type: "future_a", Path: "/one.jsonl", Line: 1, Count: 2},
		{Reason: "unknown_type", Type: "future_b", Path: "/one.jsonl", Line: 3, Count: 1},
		{Reason: "malformed_json", Path: "/two.jsonl", Line: 4, Count: 1},
	}
	var output bytes.Buffer
	writeWarnings(&output, warnings, false)
	got := output.String()
	if strings.Count(got, "warning:") != 1 {
		t.Fatalf("summary should contain one warning: %q", got)
	}
	for _, want := range []string{"skipped 4 records", "across 2 files", "unknown_type: future_a=2, future_b=1", "malformed_json=1", "--verbose"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "/one.jsonl") || strings.Contains(got, "/two.jsonl") {
		t.Fatalf("summary leaked file paths: %q", got)
	}
}

func TestWriteWarningsVerboseIncludesDetails(t *testing.T) {
	warnings := []usage.Warning{
		{Reason: "unknown_type", Type: "future_a", Path: "/one.jsonl", Line: 1, Count: 2},
		{Reason: "malformed_json", Path: "/two.jsonl", Line: 4, Count: 1},
	}
	var output bytes.Buffer
	writeWarnings(&output, warnings, true)
	got := output.String()
	for _, want := range []string{"future_a", "/one.jsonl:1", "/two.jsonl:4"} {
		if !strings.Contains(got, want) {
			t.Errorf("verbose warning missing %q: %q", want, got)
		}
	}
}

func TestWriteWarningsSeparatesUnreadableFilesFromSkippedRecords(t *testing.T) {
	warnings := []usage.Warning{
		{Reason: "read_file", Path: "/one.jsonl", Count: 1},
		{Reason: "malformed_json", Path: "/two.jsonl", Count: 2},
	}
	var output bytes.Buffer
	writeWarnings(&output, warnings, false)
	got := output.String()
	for _, want := range []string{"skipped 2 records", "could not read 1 file", "across 2 files"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q: %q", want, got)
		}
	}
}

func TestRunStrictInputReturnsNonZeroAfterRenderingReport(t *testing.T) {
	home := testHome(t)
	path := filepath.Join(home, "sessions", "2026", "one.jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("not-json\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"stats", "--codex-home", home, "--strict-input"}, &stdout, &stderr); code == 0 {
		t.Fatalf("strict-input unexpectedly succeeded: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if stdout.Len() == 0 || !strings.Contains(stderr.String(), "strict-input") {
		t.Fatalf("strict-input diagnostics missing: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
}

func TestRunStylesDiagnosticPrefixesOnly(t *testing.T) {
	home := testHome(t)
	path := filepath.Join(home, "sessions", "2026", "one.jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("not-json\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"stats", "--codex-home", home, "--color", "always"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	got := stderr.String()
	warningPrefix := "\x1b[1;93mwarning:\x1b[m "
	if !strings.HasPrefix(got, warningPrefix) {
		t.Fatalf("warning prefix is not yellow and bold: %q", got)
	}
	if strings.Contains(strings.TrimPrefix(got, warningPrefix), "\x1b[") {
		t.Fatalf("warning body is styled with its prefix: %q", got)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"stats", "--days", "0", "--color", "always"}, &stdout, &stderr); code == 0 {
		t.Fatalf("invalid days unexpectedly succeeded: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if got := stderr.String(); !strings.Contains(got, "\x1b[1;91merror:\x1b[m --days must be at least 1") {
		t.Fatalf("error prefix is not red and bold: %q", got)
	}
}

func TestRunKeepsDiagnosticsPlainForJSON(t *testing.T) {
	home := testHome(t)
	path := filepath.Join(home, "sessions", "2026", "one.jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("not-json\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"stats", "--codex-home", home, "--json", "--color", "always"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "\x1b[") || strings.Contains(stderr.String(), "\x1b[") {
		t.Fatalf("JSON diagnostics contain ANSI: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}
