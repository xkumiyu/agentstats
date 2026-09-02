package output

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/xkumiyu/agentstats/internal/aggregate"
	"github.com/xkumiyu/agentstats/internal/usage"
	"golang.org/x/term"
)

type ColorMode string

const (
	ColorAuto   ColorMode = "auto"
	ColorAlways ColorMode = "always"
	ColorNever  ColorMode = "never"
)

func (m ColorMode) Valid() bool { return m == ColorAuto || m == ColorAlways || m == ColorNever }

type TerminalCapabilities struct {
	IsTTY        bool
	Width        int
	ColorProfile string
	ColorMode    ColorMode
	NoColor      bool
}

var ansiSequenceRE = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

// DetectCapabilities gets terminal information without writing to the output.
// Tests can construct TerminalCapabilities directly for deterministic output.
func DetectCapabilities(file *os.File, mode ColorMode, noColor bool) TerminalCapabilities {
	capabilities := TerminalCapabilities{ColorMode: mode, NoColor: noColor, Width: 80}
	if file == nil {
		return capabilities
	}
	fd := int(file.Fd())
	capabilities.IsTTY = term.IsTerminal(fd)
	if width, _, err := term.GetSize(fd); err == nil && width > 0 {
		capabilities.Width = width
	}
	if capabilities.ColorMode == "" {
		capabilities.ColorMode = ColorAuto
	}
	return capabilities
}

func (c TerminalCapabilities) ColorsEnabled() bool {
	switch c.ColorMode {
	case ColorAlways:
		return true
	case ColorNever:
		return false
	default:
		return c.IsTTY && !c.NoColor
	}
}

type ReportContext struct {
	Agent         string
	Period        string
	Layer         usage.ToolLayer
	Strict        bool
	ReferenceTime time.Time
	Location      *time.Location
}

func (c ReportContext) agent() string {
	if strings.TrimSpace(c.Agent) == "" {
		return "CODEX"
	}
	return strings.ToUpper(c.Agent)
}

func (c ReportContext) agentID() string {
	if strings.TrimSpace(c.Agent) == "" {
		return "codex"
	}
	return strings.ToLower(strings.TrimSpace(c.Agent))
}

func (c ReportContext) period() string {
	if strings.TrimSpace(c.Period) == "" {
		return "all time"
	}
	return c.Period
}

// RenderHuman renders a static, non-interactive report. kind is one of stats,
// tools, or skills.
func RenderHuman(kind string, ctx ReportContext, report aggregate.Report, capabilities TerminalCapabilities) string {
	width := capabilities.Width
	if width <= 0 {
		width = 80
	}
	styled := capabilities.ColorsEnabled()
	title := "AGENTSTATS · " + ctx.agent()
	lines := []string{styleTitle(title, styled), styleContext(contextLine(kind, ctx), styled)}
	switch kind {
	case "stats":
		lines = append(lines, "", renderStats(report.Overview, width, styled))
	case "tools":
		lines = append(lines, "", styleHeading("TOOL USAGE", styled), renderTools(report.Tools, ctx, width, styled), "", styleFooter(fmt.Sprintf("Rows: %s · Total calls: %s", formatCount(len(report.Tools)), formatCount(totalToolCalls(report.Tools))), styled))
	case "skills":
		lines = append(lines, "", styleHeading("SKILL USAGE", styled), renderSkills(report.Skills, ctx, width, styled), "", styleFooter(fmt.Sprintf("Rows: %s · Total uses: %s", formatCount(len(report.Skills)), formatCount(totalSkillUses(report.Skills))), styled))
	default:
		return ""
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}

func contextLine(kind string, ctx ReportContext) string {
	parts := []string{"Period: " + ctx.period()}
	if kind == "tools" {
		layer := ctx.Layer
		if layer == "" {
			layer = usage.LayerEffective
		}
		parts = append(parts, "Layer: "+string(layer))
	}
	if kind == "skills" {
		parts = append(parts, "Strict: "+strconv.FormatBool(ctx.Strict))
	}
	return strings.Join(parts, "  ·  ")
}

func renderStats(summary aggregate.Overview, width int, styled bool) string {
	items := []string{
		metric("Sessions", summary.Sessions, styled),
		metric("User Prompts", summary.UserPrompts, styled),
		metric("Tool Calls", summary.ToolCalls, styled),
		metric("Skill Uses", summary.SkillUses, styled),
	}
	if width < 70 {
		return strings.Join(items, "\n") + "\n" + styleEmptyIfZero(summary, styled)
	}
	max := 0
	for _, item := range items {
		if w := lipgloss.Width(item); w > max {
			max = w
		}
	}
	for i := range items {
		items[i] = padRight(items[i], max)
	}
	return strings.Join(items, "  ") + "\n" + styleEmptyIfZero(summary, styled)
}

func styleEmptyIfZero(summary aggregate.Overview, styled bool) string {
	if summary.Sessions == 0 && summary.UserPrompts == 0 && summary.ToolCalls == 0 && summary.SkillUses == 0 {
		return styleNotice("No usage found for the selected period.", styled)
	}
	return ""
}

func metric(label string, value int, styled bool) string {
	return padRight(styleLabel(label, styled), 14) + " " + padLeft(formatCount(value), 12)
}

func renderTools(rows []aggregate.ToolRow, ctx ReportContext, width int, styled bool) string {
	if len(rows) == 0 {
		return styleNotice("No tool usage found for the selected period and layer.", styled)
	}
	compact := width < 70
	standard := width < 100
	if compact {
		nameWidth := maxNameWidth(width-9, rows, func(row aggregate.ToolRow) string { return safeDisplay(row.Name) })
		lines := []string{padRight(styleHeader("Tool", styled), nameWidth) + "  " + padLeft(styleHeader("Calls", styled), 7)}
		for _, row := range rows {
			lines = append(lines, padRight(truncate(safeDisplay(row.Name), nameWidth), nameWidth)+"  "+padLeft(formatCount(row.Calls), 7))
		}
		return strings.Join(lines, "\n")
	}
	nameWidth := maxNameWidth(width-44, rows, func(row aggregate.ToolRow) string { return safeDisplay(row.Name) })
	if standard {
		lines := []string{padRight(styleHeader("Tool", styled), nameWidth) + "  " + padLeft(styleHeader("Calls", styled), 8) + "  " + padLeft(styleHeader("Failures", styled), 8) + "  " + padLeft(styleHeader("Last Used", styled), 22)}
		for _, row := range rows {
			failure := formatCount(row.Failures)
			if styled && row.Failures > 0 {
				failure = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(failure)
			}
			lines = append(lines, padRight(truncate(safeDisplay(row.Name), nameWidth), nameWidth)+"  "+padLeft(formatCount(row.Calls), 8)+"  "+padLeft(failure, 8)+"  "+padLeft(formatLocalTime(row.LastUsed, ctx.Location), 22))
		}
		return strings.Join(lines, "\n")
	}
	lines := []string{padRight(styleHeader("Tool", styled), nameWidth) + "  " + padLeft(styleHeader("Calls", styled), 8) + "  " + padLeft(styleHeader("Failures", styled), 8) + "  " + padLeft(styleHeader("Last Used", styled), 22)}
	for _, row := range rows {
		failure := formatCount(row.Failures)
		if styled && row.Failures > 0 {
			failure = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(failure)
		}
		lines = append(lines, padRight(truncate(safeDisplay(row.Name), nameWidth), nameWidth)+"  "+padLeft(formatCount(row.Calls), 8)+"  "+padLeft(failure, 8)+"  "+padLeft(formatLocalTime(row.LastUsed, ctx.Location), 22))
	}
	return strings.Join(lines, "\n")
}

func renderSkills(rows []aggregate.SkillRow, ctx ReportContext, width int, styled bool) string {
	if len(rows) == 0 {
		return styleNotice("No skill usage found for the selected period and filter.", styled)
	}
	if width < 70 {
		nameWidth := maxSkillNameWidth(width-9, rows)
		lines := []string{padRight(styleHeader("Skill", styled), nameWidth) + "  " + padLeft(styleHeader("Total", styled), 7)}
		for _, row := range rows {
			lines = append(lines, padRight(truncate(safeDisplay(row.Name), nameWidth), nameWidth)+"  "+padLeft(formatCount(row.Total), 7))
		}
		return strings.Join(lines, "\n")
	}
	if width < 100 {
		nameWidth := maxSkillNameWidth(width-39, rows)
		lines := []string{padRight(styleHeader("Skill", styled), nameWidth) + "  " + padLeft(styleHeader("Explicit", styled), 8) + "  " + padLeft(styleHeader("Implicit", styled), 8) + "  " + padLeft(styleHeader("Unknown", styled), 8) + "  " + padLeft(styleHeader("Total", styled), 7)}
		for _, row := range rows {
			lines = append(lines, padRight(truncate(safeDisplay(row.Name), nameWidth), nameWidth)+"  "+padLeft(formatCount(row.Explicit), 8)+"  "+padLeft(formatCount(row.Implicit), 8)+"  "+padLeft(formatCount(row.Unknown), 8)+"  "+padLeft(formatCount(row.Total), 7))
		}
		return strings.Join(lines, "\n")
	}
	nameWidth := maxSkillNameWidth(width-97, rows)
	lines := []string{padRight(styleHeader("Skill", styled), nameWidth) + "  " + padLeft(styleHeader("Explicit", styled), 8) + "  " + padLeft(styleHeader("Implicit", styled), 8) + "  " + padLeft(styleHeader("Unknown", styled), 8) + "  " + padLeft(styleHeader("Confirmed", styled), 9) + "  " + padLeft(styleHeader("Inferred", styled), 8) + "  " + padLeft(styleHeader("Unconfirmed", styled), 11) + "  " + padLeft(styleHeader("Total", styled), 7) + "  " + padLeft(styleHeader("Last Used", styled), 22)}
	for _, row := range rows {
		lines = append(lines, padRight(truncate(safeDisplay(row.Name), nameWidth), nameWidth)+"  "+padLeft(formatCount(row.Explicit), 8)+"  "+padLeft(formatCount(row.Implicit), 8)+"  "+padLeft(formatCount(row.Unknown), 8)+"  "+padLeft(formatCount(row.Confirmed), 9)+"  "+padLeft(formatCount(row.Inferred), 8)+"  "+padLeft(formatCount(row.Unconfirmed), 11)+"  "+padLeft(formatCount(row.Total), 7)+"  "+padLeft(formatLocalTime(row.LastUsed, ctx.Location), 22))
	}
	return strings.Join(lines, "\n")
}

func maxNameWidth(available int, rows []aggregate.ToolRow, name func(aggregate.ToolRow) string) int {
	if available < 8 {
		available = 8
	}
	max := 8
	for _, row := range rows {
		if w := lipgloss.Width(name(row)); w > max {
			max = w
		}
	}
	if max > available {
		return available
	}
	return max
}

func maxSkillNameWidth(available int, rows []aggregate.SkillRow) int {
	if available < 8 {
		available = 8
	}
	max := 8
	for _, row := range rows {
		if w := lipgloss.Width(safeDisplay(row.Name)); w > max {
			max = w
		}
	}
	if max > available {
		return available
	}
	return max
}

func safeDisplay(value string) string {
	value = ansiSequenceRE.ReplaceAllString(value, "")
	var builder strings.Builder
	for _, r := range value {
		switch r {
		case '\n', '\r', '\t':
			builder.WriteRune(' ')
		default:
			if r >= 0x20 && r != 0x7f {
				builder.WriteRune(r)
			}
		}
	}
	return builder.String()
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	var result []rune
	for _, r := range value {
		candidate := string(result) + string(r) + "…"
		if lipgloss.Width(candidate) > width {
			break
		}
		result = append(result, r)
	}
	return string(result) + "…"
}

func padRight(value string, width int) string {
	padding := width - lipgloss.Width(value)
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}

func padLeft(value string, width int) string {
	padding := width - lipgloss.Width(value)
	if padding <= 0 {
		return value
	}
	return strings.Repeat(" ", padding) + value
}

func formatCount(value int) string {
	if value < 0 {
		return "-" + formatCount(-value)
	}
	s := strconv.Itoa(value)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func formatLocalTime(value time.Time, location *time.Location) string {
	if value.IsZero() {
		return "-"
	}
	if location == nil {
		location = time.Local
	}
	return value.In(location).Format("2006-01-02 15:04 MST")
}

func formatMachineTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func styleTitle(value string, enabled bool) string {
	if !enabled {
		return value
	}
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).Render(value)
}

func styleContext(value string, enabled bool) string {
	if !enabled {
		return value
	}
	return lipgloss.NewStyle().Faint(true).Render(value)
}

func styleHeading(value string, enabled bool) string {
	if !enabled {
		return value
	}
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10")).Render(value)
}

func styleHeader(value string, enabled bool) string {
	if !enabled {
		return value
	}
	return lipgloss.NewStyle().Bold(true).Render(value)
}

func styleLabel(value string, enabled bool) string {
	if !enabled {
		return value
	}
	return lipgloss.NewStyle().Bold(true).Render(value)
}

func styleFooter(value string, enabled bool) string {
	if !enabled {
		return value
	}
	return lipgloss.NewStyle().Faint(true).Render(value)
}

func styleNotice(value string, enabled bool) string {
	if !enabled {
		return value
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(value)
}

func totalToolCalls(rows []aggregate.ToolRow) int {
	result := 0
	for _, row := range rows {
		result += row.Calls
	}
	return result
}

func totalSkillUses(rows []aggregate.SkillRow) int {
	result := 0
	for _, row := range rows {
		result += row.Total
	}
	return result
}

// RenderJSON returns one stable JSON document per command.
func RenderJSON(kind string, ctx ReportContext, report aggregate.Report) ([]byte, error) {
	base := struct {
		SchemaVersion int    `json:"schema_version"`
		Agent         string `json:"agent"`
		Period        string `json:"period"`
	}{1, ctx.agentID(), ctx.period()}
	switch kind {
	case "stats":
		value := struct {
			SchemaVersion int    `json:"schema_version"`
			Agent         string `json:"agent"`
			Period        string `json:"period"`
			GeneratedAt   string `json:"generated_at"`
			Sessions      int    `json:"sessions"`
			UserPrompts   int    `json:"user_prompts"`
			ToolCalls     int    `json:"tool_calls"`
			SkillUses     int    `json:"skill_uses"`
		}{base.SchemaVersion, base.Agent, base.Period, formatMachineTime(ctx.ReferenceTime), report.Overview.Sessions, report.Overview.UserPrompts, report.Overview.ToolCalls, report.Overview.SkillUses}
		return json.MarshalIndent(value, "", "  ")
	case "tools":
		rows := make([]toolJSON, 0, len(report.Tools))
		for _, row := range report.Tools {
			rows = append(rows, toolJSON{row.Name, row.Calls, row.Failures, formatMachineTime(row.LastUsed)})
		}
		value := struct {
			SchemaVersion int        `json:"schema_version"`
			Agent         string     `json:"agent"`
			Period        string     `json:"period"`
			GeneratedAt   string     `json:"generated_at"`
			Layer         string     `json:"layer"`
			Rows          []toolJSON `json:"rows"`
		}{base.SchemaVersion, base.Agent, base.Period, formatMachineTime(ctx.ReferenceTime), string(contextLayer(ctx)), rows}
		return json.MarshalIndent(value, "", "  ")
	case "skills":
		rows := make([]skillJSON, 0, len(report.Skills))
		for _, row := range report.Skills {
			rows = append(rows, skillJSON{row.Name, row.Explicit, row.Implicit, row.Unknown, row.Confirmed, row.Inferred, row.Unconfirmed, row.Total, formatMachineTime(row.LastUsed)})
		}
		value := struct {
			SchemaVersion int         `json:"schema_version"`
			Agent         string      `json:"agent"`
			Period        string      `json:"period"`
			GeneratedAt   string      `json:"generated_at"`
			Strict        bool        `json:"strict"`
			Rows          []skillJSON `json:"rows"`
		}{base.SchemaVersion, base.Agent, base.Period, formatMachineTime(ctx.ReferenceTime), ctx.Strict, rows}
		return json.MarshalIndent(value, "", "  ")
	default:
		return nil, fmt.Errorf("unknown report kind %q", kind)
	}
}

func contextLayer(ctx ReportContext) usage.ToolLayer {
	if ctx.Layer == "" {
		return usage.LayerEffective
	}
	return ctx.Layer
}

type toolJSON struct {
	Name     string `json:"name"`
	Calls    int    `json:"calls"`
	Failures int    `json:"failures"`
	LastUsed string `json:"last_used"`
}

type skillJSON struct {
	Name        string `json:"name"`
	Explicit    int    `json:"explicit"`
	Implicit    int    `json:"implicit"`
	Unknown     int    `json:"unknown"`
	Confirmed   int    `json:"confirmed"`
	Inferred    int    `json:"inferred"`
	Unconfirmed int    `json:"unconfirmed"`
	Total       int    `json:"total"`
	LastUsed    string `json:"last_used"`
}

// RenderCSV returns a stable-header CSV document. Metadata is represented by
// the report-specific columns, keeping it easy to consume with spreadsheet and
// shell tools.
func RenderCSV(kind string, _ ReportContext, report aggregate.Report) ([]byte, error) {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	switch kind {
	case "stats":
		if err := writer.Write([]string{"sessions", "user_prompts", "tool_calls", "skill_uses"}); err != nil {
			return nil, err
		}
		if err := writer.Write([]string{strconv.Itoa(report.Overview.Sessions), strconv.Itoa(report.Overview.UserPrompts), strconv.Itoa(report.Overview.ToolCalls), strconv.Itoa(report.Overview.SkillUses)}); err != nil {
			return nil, err
		}
	case "tools":
		if err := writer.Write([]string{"name", "calls", "failures", "last_used"}); err != nil {
			return nil, err
		}
		for _, row := range report.Tools {
			if err := writer.Write([]string{row.Name, strconv.Itoa(row.Calls), strconv.Itoa(row.Failures), formatMachineTime(row.LastUsed)}); err != nil {
				return nil, err
			}
		}
	case "skills":
		if err := writer.Write([]string{"name", "explicit", "implicit", "unknown", "confirmed", "inferred", "unconfirmed", "total", "last_used"}); err != nil {
			return nil, err
		}
		for _, row := range report.Skills {
			if err := writer.Write([]string{row.Name, strconv.Itoa(row.Explicit), strconv.Itoa(row.Implicit), strconv.Itoa(row.Unknown), strconv.Itoa(row.Confirmed), strconv.Itoa(row.Inferred), strconv.Itoa(row.Unconfirmed), strconv.Itoa(row.Total), formatMachineTime(row.LastUsed)}); err != nil {
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("unknown report kind %q", kind)
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// WriteMachine writes machine output without adding ANSI sequences.
func WriteMachine(w io.Writer, kind string, format string, ctx ReportContext, report aggregate.Report) error {
	if format == "json" {
		data, err := RenderJSON(kind, ctx, report)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(data))
		return err
	}
	data, err := RenderCSV(kind, ctx, report)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// SortMethods is kept here for callers serializing evidence outside reports.
func SortMethods(methods []usage.SkillEvidenceMethod) []usage.SkillEvidenceMethod {
	result := append([]usage.SkillEvidenceMethod(nil), methods...)
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
