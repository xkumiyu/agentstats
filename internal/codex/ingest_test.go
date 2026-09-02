package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xkumiyu/agentstats/internal/usage"
)

func TestResolveHomePrecedence(t *testing.T) {
	if got, _ := ResolveHomeFrom(" /explicit ", "/env", "/user"); got != " /explicit " {
		t.Fatalf("explicit home was unexpectedly trimmed: %q", got)
	}
	if got, _ := ResolveHomeFrom("", "/env", "/user"); got != "/env" {
		t.Fatalf("env home = %q", got)
	}
	if got, _ := ResolveHomeFrom("", "", "/user"); got != "/user/.codex" {
		t.Fatalf("default home = %q", got)
	}
	if _, err := ResolveHomeFrom("", "", ""); err == nil {
		t.Fatal("empty user home should fail")
	}
}

func TestDiscoverSortsBothRootsAndIgnoresMissingRoot(t *testing.T) {
	home := t.TempDir()
	paths := []string{
		filepath.Join(home, "sessions", "z", "two.jsonl"),
		filepath.Join(home, "sessions", "a.jsonl"),
		filepath.Join(home, "archived_sessions", "one.jsonl"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Discover(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(paths) || !strings.HasSuffix(got[0], "archived_sessions/one.jsonl") {
		t.Fatalf("unexpected files: %#v", got)
	}
	if _, err := Discover(filepath.Join(home, "missing")); err == nil {
		t.Fatal("missing home should fail")
	}
}

func TestDecodeFileContinuesAfterMalformedUnknownAndLargeLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	contents := "{\"timestamp\":\"2026-01-01T00:00:00Z\",\"type\":\"session_meta\",\"payload\":{\"id\":\"s\"}}\n" +
		"not-json\n" +
		"{\"type\":\"future\"}\n" +
		strings.Repeat("x", 512) + "\n" +
		"{\"timestamp\":\"2026-01-01T00:00:01Z\",\"type\":\"task_started\",\"payload\":{\"id\":\"t\"}}\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	var got []Envelope
	warnings := &WarningCollector{}
	if err := DecodeFile(path, DecodeOptions{MaxLineBytes: 256, Warnings: warnings}, func(env Envelope) { got = append(got, env) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Source.Line != 1 || got[1].Source.Line != 5 {
		t.Fatalf("decoded envelopes = %#v", got)
	}
	if len(warnings.Warnings()) != 3 {
		t.Fatalf("warnings = %#v", warnings.Warnings())
	}
}

func TestDecodeFileSkipsKnownMetadataWithoutWarning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	contents := strings.Join([]string{
		`{"type":"session_meta","payload":{"id":"s"}}`,
		`{"type":"turn_context","payload":{"turn_id":"t","model":"gpt"}}`,
		`{"type":"world_state","payload":{"cwd":"/tmp"}}`,
		`{"type":"compacted","payload":{"turn_id":"t"}}`,
		`{"type":"inter_agent_communication_metadata","payload":{"turn_id":"t"}}`,
		`{"type":"task_started","payload":{"turn_id":"t"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	var got []Envelope
	warnings := &WarningCollector{}
	if err := DecodeFile(path, DecodeOptions{Warnings: warnings}, func(env Envelope) { got = append(got, env) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Type != "session_meta" || got[1].Type != "task_started" {
		t.Fatalf("decoded envelopes = %#v", got)
	}
	if len(warnings.Warnings()) != 0 {
		t.Fatalf("metadata warnings = %#v", warnings.Warnings())
	}
}

func TestLoadAssemblesExplicitAndOrdinalTurnsAndFlushesAbort(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "2026", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	contents := strings.Join([]string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s","project_path":"/project","cli_version":"1"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"task_started","payload":{"turn_id":"turn-a"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"event_msg","payload":{"type":"user_message","text":"hello"}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"task_complete","payload":{"turn_id":"turn-a"}}`,
		`{"timestamp":"2026-01-01T00:00:04Z","type":"user_message","payload":{"text":"next"}}`,
		`{"timestamp":"2026-01-01T00:00:05Z","type":"turn_aborted","payload":{}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 2 {
		t.Fatalf("turns = %#v", result.Turns)
	}
	if result.Turns[0].ID != "turn-a" || result.Turns[0].UserPrompts != 1 || result.Turns[1].Ordinal != 2 || !result.Turns[1].Aborted {
		t.Fatalf("assembled turns = %#v", result.Turns)
	}
	if len(result.Sessions) != 1 || result.Sessions[0].ProjectPath != "/project" {
		t.Fatalf("sessions = %#v", result.Sessions)
	}
}

func TestTimestampFilterIsCutoffInclusiveAndRejectsNegative(t *testing.T) {
	now := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	filter, err := NewTimestampFilter(2, now)
	if err != nil {
		t.Fatal(err)
	}
	if !filter.Accept(now.Add(-48*time.Hour)) || filter.Accept(now.Add(-48*time.Hour-time.Nanosecond)) {
		t.Fatal("cutoff is not inclusive")
	}
	if _, err := NewTimestampFilter(-1, now); err == nil {
		t.Fatal("negative days should fail")
	}
	if _, err := NewTimestampFilter(0, now); err == nil {
		t.Fatal("zero days should fail when a filter is requested")
	}
}

func TestLoadAttachesModelRuntimeAndSkillEvidence(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s","cli_version":"9"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"user_message","payload":{"text":"$report"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"response_item","payload":{"type":"function_call","name":"exec","call_id":"c1","arguments":"{\"cmd\":\"cat /x/skills/report/SKILL.md\"}"}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"event_msg","payload":{"type":"ItemCompleted","item":{"type":"CommandExecution","id":"i1","status":"completed","command":"cat /x/skills/report/SKILL.md"}}}`,
		`{"timestamp":"2026-01-01T00:00:04Z","type":"task_complete","payload":{}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 {
		t.Fatalf("turns = %#v", result.Turns)
	}
	turn := result.Turns[0]
	if len(turn.ModelTools) != 1 || turn.ModelTools[0].CallID != "c1" || turn.ModelTools[0].Source.CLIVersion != "9" {
		t.Fatalf("model tools = %#v", turn.ModelTools)
	}
	if len(turn.RuntimeTools) != 1 || turn.RuntimeTools[0].CanonicalName != "shell" || turn.RuntimeTools[0].Source.CLIVersion != "9" {
		t.Fatalf("runtime tools = %#v", turn.RuntimeTools)
	}
	if len(turn.SkillEvidence) < 2 {
		t.Fatalf("skill evidence = %#v", turn.SkillEvidence)
	}
}

func TestLoadRecognizesSnakeCaseItemCompletedAndImplicitSkillAccess(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"task_started","payload":{"turn_id":"t"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","id":"i1","status":"completed","command":["/bin/zsh","-lc","cat /home/user/.agents/skills/report/SKILL.md"]}}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"task_complete","payload":{"turn_id":"t"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || len(result.Turns[0].RuntimeTools) != 1 {
		t.Fatalf("runtime tools = %#v", result.Turns)
	}
	var found bool
	for _, evidence := range result.Turns[0].SkillEvidence {
		if evidence.SkillName == "report" && evidence.Mode == usage.ModeImplicit && evidence.Method == usage.MethodImplicitAccess {
			found = true
		}
	}
	if !found {
		t.Fatalf("implicit evidence = %#v", result.Turns[0].SkillEvidence)
	}
}

func TestSelectedSkillInjectionWithoutExplicitRequestIsUnknown(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"task_started","payload":{"turn_id":"t"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<skill>\n<name>report</name>\n---\nname: report\n---\nbody\n</skill>"}],"internal_chat_message_metadata_passthrough":{"content_item_kinds":["skills.selected_skill_instructions"]}}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"task_complete","payload":{"turn_id":"t"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || len(result.Turns[0].SkillEvidence) != 1 {
		t.Fatalf("skill evidence = %#v", result.Turns)
	}
	evidence := result.Turns[0].SkillEvidence[0]
	if evidence.SkillName != "report" || evidence.Mode != usage.ModeUnknown || evidence.Method != usage.MethodSelectedSkillInstructions || evidence.State != usage.StateConfirmed {
		t.Fatalf("selected evidence = %#v", evidence)
	}
}

func TestSelectedSkillInjectionStaysInPromptTurn(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"$report"}],"internal_chat_message_metadata_passthrough":{"turn_id":"t","content_item_kinds":["user.text"]}}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"event_msg","payload":{"type":"item_completed","turn_id":"t","item":{"type":"UserMessage","content":[{"type":"text","text":"$report"},{"type":"skill","name":"report","path":"/home/user/.agents/skills/report/SKILL.md"}]}}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<skill><name>report</name><path>/home/user/.agents/skills/report/SKILL.md</path></skill>"}],"internal_chat_message_metadata_passthrough":{"turn_id":"t","content_item_kinds":["skills.selected_skill_instructions"]}}}`,
		`{"timestamp":"2026-01-01T00:00:04Z","type":"task_complete","payload":{"turn_id":"t"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || result.Turns[0].ID != "t" || result.Turns[0].UserPrompts != 1 {
		t.Fatalf("turns = %#v", result.Turns)
	}
	uses := usage.MergeSkillEvidence(result.Turns[0].SkillEvidence)
	if len(uses) != 1 || !uses[0].HasMode(usage.ModeExplicit) || uses[0].HasMode(usage.ModeUnknown) || uses[0].State != usage.StateConfirmed {
		t.Fatalf("merged skill uses = %#v", uses)
	}
}

func TestResponseItemUserMessageAndInjectedSkillAreNotPrompt(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s"}}`,
		`{"timestamp":"2026-01-01T00:00:00Z","type":"task_started","payload":{"turn_id":"injected"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<skill name=\"report\">body</skill>"}]}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"task_complete","payload":{"turn_id":"injected"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"task_started","payload":{"turn_id":"actual"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"task_complete","payload":{"turn_id":"actual"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 2 || result.Turns[0].UserPrompts != 0 || result.Turns[1].UserPrompts != 1 {
		t.Fatalf("turns = %#v", result.Turns)
	}
}

func TestInjectedSkillWithUserTextStillCountsPrompt(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"user_message","payload":{"text":"<skill name=\"report\">body</skill>\nplease summarize"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"task_complete","payload":{}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || result.Turns[0].UserPrompts != 1 {
		t.Fatalf("turns = %#v", result.Turns)
	}
}

func TestItemIDsDoNotCreateExtraTurns(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "one.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"s"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"task_started","payload":{"turn_id":"t"}}`,
		`{"timestamp":"2026-01-01T00:00:02Z","type":"response_item","payload":{"type":"function_call","id":"item-id","name":"exec","call_id":"call-id"}}`,
		`{"timestamp":"2026-01-01T00:00:03Z","type":"event_msg","payload":{"type":"ItemCompleted","item":{"type":"CommandExecution","id":"runtime-id","command":"true"}}}`,
		`{"timestamp":"2026-01-01T00:00:04Z","type":"task_complete","payload":{"turn_id":"t"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Load(home, IngestOptions{Now: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 1 || len(result.Turns[0].ModelTools) != 1 || len(result.Turns[0].RuntimeTools) != 1 {
		t.Fatalf("turns = %#v", result.Turns)
	}
}
