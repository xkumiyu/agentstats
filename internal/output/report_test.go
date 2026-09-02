package output

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/xkumiyu/agentstats/internal/aggregate"
	"github.com/xkumiyu/agentstats/internal/usage"
)

func sampleReport() aggregate.Report {
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	return aggregate.Report{
		Overview: aggregate.Overview{Sessions: 12_345, UserPrompts: 234, ToolCalls: 56_789, SkillUses: 42},
		Tools:    []aggregate.ToolRow{{Name: "shell", Calls: 56_789, Failures: 2, LastUsed: stamp}},
		Skills:   []aggregate.SkillRow{{Name: "very-long-skill-name-日本語", Explicit: 2, Implicit: 1, Unknown: 1, Confirmed: 2, Inferred: 1, Unconfirmed: 0, Total: 3, LastUsed: stamp}},
	}
}

func TestRenderHumanPlainReportIsReadable(t *testing.T) {
	ctx := ReportContext{Agent: "codex", Period: "last 30 days", Layer: usage.LayerEffective, ReferenceTime: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), Location: time.FixedZone("JST", 9*60*60)}
	got := RenderHuman("tools", ctx, sampleReport(), TerminalCapabilities{Width: 120, ColorMode: ColorNever})
	for _, want := range []string{"AGENTSTATS · CODEX", "Period: last 30 days", "Layer: effective", "Tool", "shell", "56,789", "2026-01-02 12:04 JST", "Total calls: 56,789"} {
		if !strings.Contains(got, want) {
			t.Errorf("report does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("plain report contains ANSI: %q", got)
	}
}

func TestStandardToolLayoutKeepsLastUsed(t *testing.T) {
	ctx := ReportContext{Agent: "codex", Period: "all time", Layer: usage.LayerEffective, Location: time.UTC}
	got := RenderHuman("tools", ctx, sampleReport(), TerminalCapabilities{Width: 80, ColorMode: ColorNever})
	if !strings.Contains(got, "Last Used") || lipgloss.Width(strings.Split(strings.TrimSpace(got), "\n")[4]) > 80 {
		t.Fatalf("standard layout = %q", got)
	}
}

func TestSkillReportPrioritizesReadableNames(t *testing.T) {
	ctx := ReportContext{Agent: "codex", Period: "all time", SkillGroupBy: aggregate.SkillGroupByTurn}
	report := aggregate.Report{Skills: []aggregate.SkillRow{
		{Name: "openspec-apply-change", Total: 90},
		{Name: "git-workflow-and-versioning", Total: 85},
	}}
	got := RenderHuman("skills", ctx, report, TerminalCapabilities{Width: 105, ColorMode: ColorNever})
	for _, name := range []string{"openspec-apply-change", "git-workflow-and-versioning"} {
		if !strings.Contains(got, name) {
			t.Errorf("skill name %q was truncated too aggressively:\n%s", name, got)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(got), "\n") {
		if lipgloss.Width(line) > 105 {
			t.Errorf("line too wide: %d: %q", lipgloss.Width(line), line)
		}
	}
}

func TestRenderHumanAlwaysColorAndCompactEllipsis(t *testing.T) {
	ctx := ReportContext{Agent: "codex", Period: "all time", Layer: usage.LayerEffective}
	colored := RenderHuman("skills", ctx, sampleReport(), TerminalCapabilities{Width: 120, ColorMode: ColorAlways})
	if !strings.Contains(colored, "\x1b[") {
		t.Fatalf("always mode did not style report: %q", colored)
	}
	long := sampleReport()
	long.Skills[0].Name = "very-long-skill-name-日本語-with-a-name-that-needs-ellipsis"
	compact := RenderHuman("skills", ctx, long, TerminalCapabilities{Width: 60, ColorMode: ColorNever})
	if !strings.Contains(compact, "Skill") || !strings.Contains(compact, "Total") || !strings.Contains(compact, "…") {
		t.Fatalf("compact report lost required fields: %q", compact)
	}
	for _, line := range strings.Split(strings.TrimSpace(compact), "\n") {
		if lipgloss.Width(line) > 60 {
			t.Errorf("compact line too wide: %d: %q", lipgloss.Width(line), line)
		}
	}
	wide := RenderHuman("skills", ctx, long, TerminalCapabilities{Width: 120, ColorMode: ColorNever})
	for _, line := range strings.Split(strings.TrimSpace(wide), "\n") {
		if lipgloss.Width(line) > 120 {
			t.Errorf("wide line too wide: %d: %q", lipgloss.Width(line), line)
		}
	}
}

func TestColorModeRespectsTTYAndNOColor(t *testing.T) {
	ctx := ReportContext{Agent: "codex", Period: "all time"}
	report := sampleReport()
	if plain := RenderHuman("stats", ctx, report, TerminalCapabilities{IsTTY: false, ColorMode: ColorAuto}); strings.Contains(plain, "\x1b[") {
		t.Fatal("auto mode styled redirected output")
	}
	if plain := RenderHuman("stats", ctx, report, TerminalCapabilities{IsTTY: true, NoColor: true, ColorMode: ColorAuto}); strings.Contains(plain, "\x1b[") {
		t.Fatal("auto mode ignored NO_COLOR")
	}
	if colored := RenderHuman("stats", ctx, report, TerminalCapabilities{IsTTY: false, NoColor: true, ColorMode: ColorAlways}); !strings.Contains(colored, "\x1b[") {
		t.Fatal("always mode did not override TTY/NO_COLOR")
	}
}

func TestMachineRenderersNeverEmitANSIAndKeepEmptyArrays(t *testing.T) {
	ctx := ReportContext{Agent: "codex", Period: "all time", Layer: usage.LayerEffective, ReferenceTime: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)}
	empty := aggregate.Report{}
	data, err := RenderJSON("tools", ctx, empty)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "\x1b[") || !strings.Contains(string(data), `"rows": []`) {
		t.Fatalf("JSON = %s", data)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
}

func TestSkillReportShowsUnknownActivationMode(t *testing.T) {
	ctx := ReportContext{Agent: "codex", Period: "all time", Strict: false}
	report := aggregate.Report{Skills: []aggregate.SkillRow{{Name: "report", Unknown: 1, Total: 1}}}
	for _, width := range []int{80, 120} {
		got := RenderHuman("skills", ctx, report, TerminalCapabilities{Width: width, ColorMode: ColorNever})
		if !strings.Contains(got, "Unknown") || !strings.Contains(got, "report") || !strings.Contains(got, "Total") {
			t.Fatalf("width %d omitted unknown mode: %s", width, got)
		}
	}
}

func TestSkillReportShowsGroupingUnit(t *testing.T) {
	ctx := ReportContext{Agent: "codex", Period: "all time", SkillGroupBy: aggregate.SkillGroupBySession}
	report := aggregate.Report{Skills: []aggregate.SkillRow{{Name: "report", Total: 1}}}
	got := RenderHuman("skills", ctx, report, TerminalCapabilities{Width: 80, ColorMode: ColorNever})
	if !strings.Contains(got, "Group by: session") {
		t.Fatalf("grouping unit missing: %s", got)
	}
}

func TestFormatCountAndTruncate(t *testing.T) {
	if got := formatCount(1_234_567); got != "1,234,567" {
		t.Fatalf("formatCount = %q", got)
	}
	if got := truncate("日本語のskill", 6); !strings.HasSuffix(got, "…") || lipgloss.Width(got) > 6 {
		t.Fatalf("truncate = %q", got)
	}
}
