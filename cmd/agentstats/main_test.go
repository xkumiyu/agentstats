package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xkumiyu/agentstats/internal/usage"
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
	for _, want := range []string{"AGENTSTATS · CODEX", "Sessions", "User Prompts", "Tool Calls", "Skill Uses"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stats missing %q: %s", want, stdout.String())
		}
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
	if code := run([]string{"tools", "--codex-home", home, "--color", "never"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "shell") || !strings.Contains(stdout.String(), "Total calls: 1") {
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

func TestRunRejectsInvalidArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"unknown"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("unknown command exit=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"stats", "--json", "--csv"}, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "cannot be used together") {
		t.Fatalf("format conflict exit=%d stderr=%s", code, stderr.String())
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
