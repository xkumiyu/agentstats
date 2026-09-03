package output

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/xkumiyu/agentstats/internal/aggregate"
	"github.com/xkumiyu/agentstats/internal/skillinventory"
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
	for _, want := range []string{"TOOL USAGE", "Agent: Codex", "Period: last 30 days", "Layer: effective", "Tool", "shell", "56,789", "2026-01-02 12:04 JST", "1 tool, 56,789 calls total"} {
		if !strings.Contains(got, want) {
			t.Errorf("report does not contain %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"AGENTSTATS", "Rows:", " · "} {
		if strings.Contains(got, unwanted) {
			t.Errorf("report contains obsolete display %q:\n%s", unwanted, got)
		}
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("plain report contains ANSI: %q", got)
	}
}

func TestRenderHumanUsesContentHeadingAndMultilineContext(t *testing.T) {
	ctx := ReportContext{Agent: "codex", Period: "last 7 days", Layer: usage.LayerRuntime, SkillGroupBy: aggregate.SkillGroupBySession, Strict: true}

	tests := []struct {
		kind string
		want []string
	}{
		{kind: "stats", want: []string{"USAGE OVERVIEW", "Agent: Codex", "Period: last 7 days", "Skill grouping: session"}},
		{kind: "tools", want: []string{"TOOL USAGE", "Agent: Codex", "Period: last 7 days", "Layer: runtime"}},
		{kind: "skills", want: []string{"SKILL USAGE", "Agent: Codex", "Period: last 7 days", "Group by: session", "Strict: true"}},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			got := RenderHuman(tt.kind, ctx, sampleReport(), TerminalCapabilities{Width: 120, ColorMode: ColorNever})
			lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
			for i, want := range tt.want {
				if i >= len(lines) || lines[i] != want {
					t.Fatalf("line %d = %q, want %q:\n%s", i, lineAt(lines, i), want, got)
				}
			}
			if strings.Contains(got, " · ") {
				t.Fatalf("context uses a middle-dot separator: %q", got)
			}
		})
	}
}

func lineAt(lines []string, index int) string {
	if index < 0 || index >= len(lines) {
		return "<missing>"
	}
	return lines[index]
}

func TestRenderHumanFootersUseDomainTerms(t *testing.T) {
	ctx := ReportContext{Agent: "codex", Period: "all time", Layer: usage.LayerEffective}
	tests := []struct {
		name   string
		kind   string
		report aggregate.Report
		want   string
	}{
		{name: "one tool", kind: "tools", report: sampleReport(), want: "1 tool, 56,789 calls total"},
		{name: "multiple tools", kind: "tools", report: aggregate.Report{Tools: []aggregate.ToolRow{{Name: "shell", Calls: 1}, {Name: "web", Calls: 2}}}, want: "2 tools, 3 calls total"},
		{name: "no tools", kind: "tools", report: aggregate.Report{}, want: "0 tools, 0 calls total"},
		{name: "one skill", kind: "skills", report: sampleReport(), want: "1 skill, 3 uses total"},
		{name: "multiple skills", kind: "skills", report: aggregate.Report{Skills: []aggregate.SkillRow{{Name: "one", Total: 1}, {Name: "two", Total: 2}}}, want: "2 skills, 3 uses total"},
		{name: "no skills", kind: "skills", report: aggregate.Report{}, want: "0 skills, 0 uses total"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderHuman(tt.kind, ctx, tt.report, TerminalCapabilities{Width: 120, ColorMode: ColorNever})
			if !strings.Contains(got, tt.want) {
				t.Fatalf("report does not contain %q:\n%s", tt.want, got)
			}
			if strings.Contains(got, "Rows:") || strings.Contains(got, " · ") {
				t.Fatalf("report contains obsolete footer syntax:\n%s", got)
			}
		})
	}
}

func TestRenderHumanKeepsRequiredFieldsAtSupportedWidths(t *testing.T) {
	ctx := ReportContext{Agent: "codex", Period: "last 30 days", Layer: usage.LayerEffective, SkillGroupBy: aggregate.SkillGroupByTurn}
	tests := []struct {
		kind     string
		required []string
	}{
		{kind: "stats", required: []string{"USAGE OVERVIEW", "Agent: Codex", "Period: last 30 days", "Sessions"}},
		{kind: "tools", required: []string{"TOOL USAGE", "Agent: Codex", "Period: last 30 days", "Tool", "Calls"}},
		{kind: "skills", required: []string{"SKILL USAGE", "Agent: Codex", "Period: last 30 days", "Skill", "Total"}},
	}

	for _, tt := range tests {
		for _, width := range []int{60, 80, 120} {
			t.Run(tt.kind+"/"+strconv.Itoa(width), func(t *testing.T) {
				got := RenderHuman(tt.kind, ctx, sampleReport(), TerminalCapabilities{Width: width, ColorMode: ColorNever})
				for _, want := range tt.required {
					if !strings.Contains(got, want) {
						t.Errorf("report does not contain %q:\n%s", want, got)
					}
				}
				for _, line := range strings.Split(strings.TrimRight(got, "\n"), "\n") {
					if lipgloss.Width(line) > width {
						t.Errorf("line width %d exceeds %d: %q", lipgloss.Width(line), width, line)
					}
				}
			})
		}
	}
}

func TestRenderHumanEmptyStateExplainsNoUsage(t *testing.T) {
	ctx := ReportContext{Agent: "codex", Period: "last 1 day", Layer: usage.LayerEffective}

	stats := RenderHuman("stats", ctx, aggregate.Report{}, TerminalCapabilities{Width: 80, ColorMode: ColorNever})
	for _, want := range []string{"USAGE OVERVIEW", "Agent: Codex", "No usage found for the selected period."} {
		if !strings.Contains(stats, want) {
			t.Errorf("empty stats does not contain %q:\n%s", want, stats)
		}
	}

	tools := RenderHuman("tools", ctx, aggregate.Report{}, TerminalCapabilities{Width: 80, ColorMode: ColorNever})
	for _, want := range []string{"No tool usage found for the selected period and layer.", "0 tools, 0 calls total"} {
		if !strings.Contains(tools, want) {
			t.Errorf("empty tools does not contain %q:\n%s", want, tools)
		}
	}

	skills := RenderHuman("skills", ctx, aggregate.Report{}, TerminalCapabilities{Width: 80, ColorMode: ColorNever})
	for _, want := range []string{"No skill usage found for the selected period and filter.", "0 skills, 0 uses total"} {
		if !strings.Contains(skills, want) {
			t.Errorf("empty skills does not contain %q:\n%s", want, skills)
		}
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

func TestSkillUsageViewsShowEffectiveView(t *testing.T) {
	ctx := ReportContext{Agent: "codex", Period: "all time", Location: time.UTC}
	wideReport := sampleReport()
	wideReport.Skills[0].Name = "report"
	tests := []struct {
		name   string
		view   SkillUsageView
		width  int
		report aggregate.Report
		want   []string
		omit   []string
	}{
		{
			name:   "auto compact",
			view:   SkillUsageViewAuto,
			width:  60,
			report: sampleReport(),
			want:   []string{"View: auto (selected: compact)", "Skill", "Total"},
			omit:   []string{"Explicit", "Confirmed"},
		},
		{
			name:   "auto mode",
			view:   SkillUsageViewAuto,
			width:  80,
			report: sampleReport(),
			want:   []string{"View: auto (selected: mode)", "Explicit", "Implicit", "Unknown", "Total"},
			omit:   []string{"Confirmed", "Last Used"},
		},
		{
			name:   "auto all",
			view:   SkillUsageViewAuto,
			width:  120,
			report: wideReport,
			want:   []string{"View: auto (selected: all)", "Explicit", "Confirmed", "Inferred", "Unconfirmed", "Last Used"},
		},
		{
			name:   "explicit mode",
			view:   SkillUsageViewMode,
			width:  80,
			report: sampleReport(),
			want:   []string{"View: mode", "Explicit", "Implicit", "Unknown", "Total"},
			omit:   []string{"Confirmed", "Last Used"},
		},
		{
			name:   "explicit state",
			view:   SkillUsageViewState,
			width:  80,
			report: sampleReport(),
			want:   []string{"View: state", "Confirmed", "Inferred", "Unconfirmed", "Total", "Last Used"},
			omit:   []string{"Explicit", "Implicit", "Unknown"},
		},
		{
			name:   "explicit all",
			view:   SkillUsageViewAll,
			width:  80,
			report: sampleReport(),
			want:   []string{"View: all", "ACTIVATION MODE", "EVIDENCE STATE", "Explicit", "Confirmed", "Last Used"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderHuman("skills", ctxWithSkillUsageView(ctx, tt.view), tt.report, TerminalCapabilities{Width: tt.width, ColorMode: ColorNever})
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("report does not contain %q:\n%s", want, got)
				}
			}
			for _, omit := range tt.omit {
				if strings.Contains(got, omit) {
					t.Errorf("report contains %q:\n%s", omit, got)
				}
			}
			for _, line := range strings.Split(strings.TrimRight(got, "\n"), "\n") {
				if lipgloss.Width(line) > tt.width {
					t.Errorf("line width %d exceeds %d: %q", lipgloss.Width(line), tt.width, line)
				}
			}
		})
	}
}

func ctxWithSkillUsageView(ctx ReportContext, view SkillUsageView) ReportContext {
	ctx.SkillUsageView = view
	return ctx
}

func TestSkillUsageViewValidation(t *testing.T) {
	for _, view := range []SkillUsageView{SkillUsageViewAuto, SkillUsageViewCompact, SkillUsageViewMode, SkillUsageViewState, SkillUsageViewAll} {
		if !view.Valid() {
			t.Errorf("%q should be valid", view)
		}
	}
	if SkillUsageView("invalid").Valid() {
		t.Fatal("invalid skill usage view accepted")
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

func TestRenderHumanStylesTableHeadersAndIdentity(t *testing.T) {
	ctx := ReportContext{Agent: "codex", Period: "all time", Layer: usage.LayerEffective}
	report := aggregate.Report{Skills: []aggregate.SkillRow{{Name: "report", Explicit: 1, Total: 1}}}
	got := RenderHuman("skills", ctx, report, TerminalCapabilities{Width: 120, ColorMode: ColorAlways})
	header := ""
	headerIndex := -1
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "Skill") && strings.Contains(line, "Explicit") {
			header = line
			break
		}
	}
	if header == "" {
		t.Fatalf("skill table header not found:\n%s", got)
	}
	lines := strings.Split(got, "\n")
	for i, line := range lines {
		if line == header {
			headerIndex = i
			break
		}
	}
	for _, label := range []string{"Skill", "Explicit", "Implicit", "Unknown", "Confirmed", "Inferred", "Unconfirmed", "Total", "Last Used"} {
		want := "\x1b[1;93m" + label + "\x1b[m"
		if !strings.Contains(header, want) {
			t.Errorf("table header %q is not yellow and bold: %q", label, header)
		}
	}
	if headerIndex < 0 || headerIndex+1 >= len(lines) || !strings.Contains(lines[headerIndex+1], "──") || !strings.Contains(lines[headerIndex+1], "\x1b[2m") {
		t.Fatalf("table header separator is missing or not faint: %q", lineAt(lines, headerIndex+1))
	}
	if !strings.Contains(got, "\x1b[96mreport\x1b[m") {
		t.Fatalf("first column identity is not cyan: %q", got)
	}
	if strings.Contains(header, "\x1b[1;4m") {
		t.Fatalf("table header still uses underline: %q", header)
	}
}

func TestRenderHumanKeepsTableStatusCellsUnstyled(t *testing.T) {
	ctx := ReportContext{Agent: "codex", Period: "all time", Layer: usage.LayerEffective, Location: time.UTC}
	noFailure := aggregate.Report{Tools: []aggregate.ToolRow{{Name: "shell", Calls: 1}}}
	withFailure := aggregate.Report{Tools: []aggregate.ToolRow{{Name: "shell", Calls: 1, Failures: 1}}}
	plain := TerminalCapabilities{Width: 120, ColorMode: ColorAlways}

	base := RenderHuman("tools", ctx, noFailure, plain)
	failure := RenderHuman("tools", ctx, withFailure, plain)
	if ansiCount(base) == 0 {
		t.Fatalf("styled report has no ANSI sequences: %q", base)
	}
	if ansiCount(failure) != ansiCount(base) {
		t.Fatalf("failure cell added styling: base=%d failure=%d\n%s", ansiCount(base), ansiCount(failure), failure)
	}

	confirmed := aggregate.Report{Skills: []aggregate.SkillRow{{Name: "report", Confirmed: 1, Total: 1}}}
	uncertain := aggregate.Report{Skills: []aggregate.SkillRow{{Name: "report", Unknown: 1, Inferred: 1, Unconfirmed: 1, Total: 1}}}
	confirmedOutput := RenderHuman("skills", ctx, confirmed, plain)
	uncertainOutput := RenderHuman("skills", ctx, uncertain, plain)
	if ansiCount(uncertainOutput) != ansiCount(confirmedOutput) {
		t.Fatalf("uncertain evidence added cell styling: confirmed=%d uncertain=%d\n%s", ansiCount(confirmedOutput), ansiCount(uncertainOutput), uncertainOutput)
	}
}

func TestDiagnosticPrefixUsesSemanticColor(t *testing.T) {
	tests := []struct {
		name         string
		level        string
		capabilities TerminalCapabilities
		want         string
	}{
		{name: "warning", level: "warning", capabilities: TerminalCapabilities{ColorMode: ColorAlways}, want: "\x1b[1;93mwarning:\x1b[m"},
		{name: "error", level: "error", capabilities: TerminalCapabilities{ColorMode: ColorAlways}, want: "\x1b[1;91merror:\x1b[m"},
		{name: "never", level: "warning", capabilities: TerminalCapabilities{ColorMode: ColorNever}, want: "warning:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DiagnosticPrefix(tt.level, tt.capabilities); got != tt.want {
				t.Fatalf("DiagnosticPrefix(%q) = %q, want %q", tt.level, got, tt.want)
			}
		})
	}
}

func ansiCount(value string) int {
	return len(ansiSequenceRE.FindAllString(value, -1))
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

func TestUnusedSkillJSONHasDiscriminatedInventoryRows(t *testing.T) {
	ctx := ReportContext{
		Agent:     "codex",
		Period:    "last 30 days",
		Strict:    true,
		SkillView: SkillViewUnused,
	}
	report := aggregate.Report{
		UnusedSkills: []skillinventory.InventoryEntry{
			{Name: "alpha", Path: "/skills/alpha-dir", NameSource: skillinventory.NameSourceFrontmatter, NameMismatch: true},
		},
		InstalledSkills: 2,
		UnusedRoots:     []string{"/skills"},
	}
	data, err := RenderJSON("skills", ctx, report)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		View           string   `json:"view"`
		Roots          []string `json:"roots"`
		InstalledCount int      `json:"installed_count"`
		UnusedCount    int      `json:"unused_count"`
		Rows           []struct {
			Name         string `json:"name"`
			Path         string `json:"path"`
			NameSource   string `json:"name_source"`
			NameMismatch bool   `json:"name_mismatch"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.View != "unused" || !reflect.DeepEqual(decoded.Roots, []string{"/skills"}) || decoded.InstalledCount != 2 || decoded.UnusedCount != 1 {
		t.Fatalf("unused JSON metadata = %#v", decoded)
	}
	if len(decoded.Rows) != 1 || decoded.Rows[0].Name != "alpha" || decoded.Rows[0].Path != "/skills/alpha-dir" || decoded.Rows[0].NameSource != "frontmatter" || !decoded.Rows[0].NameMismatch {
		t.Fatalf("unused JSON rows = %#v", decoded.Rows)
	}
	if strings.Contains(string(data), "directory_name") || strings.Contains(string(data), "explicit") || strings.Contains(string(data), "total") {
		t.Fatalf("unused JSON contains usage row fields: %s", data)
	}
}

func TestUnusedSkillJSONUsesAnEmptyArray(t *testing.T) {
	ctx := ReportContext{Agent: "codex", Period: "all time", SkillView: SkillViewUnused}
	data, err := RenderJSON("skills", ctx, aggregate.Report{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"rows": []`) {
		t.Fatalf("unused JSON rows is not an empty array: %s", data)
	}
}

func TestUnusedSkillHumanReportShowsScopeIdentityAndCounts(t *testing.T) {
	ctx := ReportContext{
		Agent:      "codex",
		Period:     "last 30 days",
		Strict:     true,
		SkillView:  SkillViewUnused,
		SkillRoots: []string{"/workspace/.agents/skills"},
	}
	report := aggregate.Report{
		UnusedSkills: []skillinventory.InventoryEntry{{
			Name:         "canonical-name",
			Path:         "/workspace/.agents/skills/directory-name",
			NameSource:   skillinventory.NameSourceFrontmatter,
			NameMismatch: true,
		}},
		InstalledSkills: 2,
	}
	got := RenderHuman("skills", ctx, report, TerminalCapabilities{Width: 120, ColorMode: ColorNever})
	for _, want := range []string{"UNUSED SKILLS", "Period: last 30 days", "Strict: true", "Roots: /workspace/.agents/skills", "canonical-name", "/workspace/.agents/skills/directory-name", "1 unused skill, 2 installed skills total"} {
		if !strings.Contains(got, want) {
			t.Errorf("unused report does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "SKILL USAGE") {
		t.Fatalf("unused report used usage heading: %s", got)
	}
	if strings.Contains(got, "Rows:") || strings.Contains(got, " · ") {
		t.Fatalf("unused report contains obsolete footer syntax: %s", got)
	}
	if strings.Contains(got, "Directory") {
		t.Fatalf("unused report contains redundant Directory column: %s", got)
	}
}

func TestUnusedSkillHumanReportDistinguishesEmptyInventoryAndAllUsed(t *testing.T) {
	ctx := ReportContext{Agent: "codex", SkillView: SkillViewUnused, SkillRoots: []string{"/skills"}}
	empty := RenderHuman("skills", ctx, aggregate.Report{}, TerminalCapabilities{Width: 80, ColorMode: ColorNever})
	if !strings.Contains(empty, "No installed skills found for the selected scope.") {
		t.Fatalf("empty inventory message = %s", empty)
	}
	allUsed := RenderHuman("skills", ctx, aggregate.Report{InstalledSkills: 2}, TerminalCapabilities{Width: 80, ColorMode: ColorNever})
	if !strings.Contains(allUsed, "No unused skills found for the selected scope and history filter.") {
		t.Fatalf("all-used message = %s", allUsed)
	}
}

func TestUnusedSkillHumanReportFitsNarrowTerminal(t *testing.T) {
	ctx := ReportContext{Agent: "codex", SkillView: SkillViewUnused, SkillRoots: []string{"/skills"}}
	report := aggregate.Report{UnusedSkills: []skillinventory.InventoryEntry{{
		Name:       "very-long-unused-skill-name-日本語",
		Path:       "/workspace/.agents/skills/very-long-directory-name",
		NameSource: skillinventory.NameSourceDirectory,
	}}}
	got := RenderHuman("skills", ctx, report, TerminalCapabilities{Width: 60, ColorMode: ColorNever})
	if !strings.Contains(got, "Skill") || !strings.Contains(got, "Path") || !strings.Contains(got, "…") {
		t.Fatalf("narrow unused report lost required fields: %s", got)
	}
	for _, line := range strings.Split(strings.TrimSpace(got), "\n") {
		if lipgloss.Width(line) > 60 {
			t.Errorf("narrow unused line too wide: %d: %q", lipgloss.Width(line), line)
		}
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
