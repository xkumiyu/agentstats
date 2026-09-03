package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/xkumiyu/agentstats/internal/aggregate"
	"github.com/xkumiyu/agentstats/internal/codex"
	"github.com/xkumiyu/agentstats/internal/output"
	"github.com/xkumiyu/agentstats/internal/skillinventory"
	"github.com/xkumiyu/agentstats/internal/usage"
	appversion "github.com/xkumiyu/agentstats/internal/version"
)

const usageText = `Usage:
  agentstats <command> [options]
  agentstats --help
  agentstats --version

Commands:
  stats     Show an overview of Codex usage
  tools     Show tool usage by canonical name
  skills    Show skill usage and evidence state

Options:
  --help       Show this help
  --version    Show the agentstats version

Run "agentstats <command> --help" for command-specific options.
`

const statsUsageText = `Usage: agentstats stats [options]

Show an overview of Codex usage.

Options:
  --days N          Include the last N days (N >= 1)
  --codex-home PATH Read a specific Codex home
  --color MODE      auto, always, or never (human report only)
  --group-by UNIT   turn or session
  --verbose         Show input warning details
  --strict-input    Exit non-zero when input records are skipped
  --json            Emit JSON
  --help            Show this help
`

const toolsUsageText = `Usage: agentstats tools [options]

Show tool usage by canonical name.

Options:
  --days N          Include the last N days (N >= 1)
  --codex-home PATH Read a specific Codex home
  --color MODE      auto, always, or never (human report only)
  --layer LAYER     effective, runtime, or model
  --verbose         Show input warning details
  --strict-input    Exit non-zero when input records are skipped
  --json            Emit JSON
  --help            Show this help
`

const skillsUsageText = `Usage: agentstats skills [options]

Show skill usage and evidence state.

Options:
  --days N          Include the last N days (N >= 1)
  --codex-home PATH Read a specific Codex home
  --color MODE      auto, always, or never (human report only)
  --group-by UNIT   turn or session
  --strict          Count confirmed skill evidence only
  --unused          Show installed skills with no recorded usage
  --root PATH       Scan a skill root (repeatable; only with --unused)
  --verbose         Show input warning details
  --strict-input    Exit non-zero when input records are skipped
  --json            Emit JSON
  --help            Show this help
`

func commandUsage(kind string) string {
	switch kind {
	case "stats":
		return statsUsageText
	case "tools":
		return toolsUsageText
	case "skills":
		return skillsUsageText
	default:
		return usageText
	}
}

func hasOption(args []string, option string) bool {
	for _, arg := range args {
		if arg == option || strings.HasPrefix(arg, option+"=") {
			return true
		}
	}
	return false
}

type stringList []string

func (values *stringList) String() string {
	return strings.Join(*values, ",")
}

func (values *stringList) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("skill root is empty")
	}
	*values = append(*values, value)
	return nil
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	_, noColor := os.LookupEnv("NO_COLOR")
	diagnostics := newDiagnosticWriter(stderr, output.ColorAuto, noColor)
	if len(args) == 0 {
		_, _ = io.WriteString(stderr, usageText)
		return 2
	}
	kind := strings.ToLower(args[0])
	if kind == "help" || kind == "--help" || kind == "-h" {
		if kind == "help" && len(args) > 1 {
			requested := strings.ToLower(args[1])
			if requested == "stats" || requested == "tools" || requested == "skills" {
				_, _ = io.WriteString(stdout, commandUsage(requested))
				return 0
			}
		}
		_, _ = io.WriteString(stdout, usageText)
		return 0
	}
	if kind == "--version" {
		_, _ = fmt.Fprintf(stdout, "agentstats %s\n", appversion.String())
		return 0
	}
	if kind != "stats" && kind != "tools" && kind != "skills" {
		diagnostics.errorf("unknown command %q", args[0])
		_, _ = io.WriteString(stderr, "\n"+usageText)
		return 2
	}
	if hasOption(args[1:], "--help") || hasOption(args[1:], "-h") {
		_, _ = io.WriteString(stdout, commandUsage(kind))
		return 0
	}
	if hasOption(args[1:], "--version") {
		diagnostics.errorf("--version is a top-level option; use agentstats --version")
		return 2
	}

	flags := flag.NewFlagSet(kind, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { _, _ = fmt.Fprint(stderr, commandUsage(kind)) }
	days := flags.Int("days", 0, "include the last N days")
	codexHome := flags.String("codex-home", "", "Codex home path")
	color := flags.String("color", string(output.ColorAuto), "human report color mode")
	layer := flags.String("layer", string(usage.LayerEffective), "tool layer")
	groupBy := flags.String("group-by", string(aggregate.SkillGroupByTurn), "skill aggregation unit")
	strict := flags.Bool("strict", false, "count confirmed skills only")
	unused := flags.Bool("unused", false, "show installed skills with no recorded usage")
	var roots stringList
	flags.Var(&roots, "root", "scan a skill root (repeatable; only with --unused)")
	verbose := flags.Bool("verbose", false, "show input warning details")
	strictInput := flags.Bool("strict-input", false, "exit non-zero when input records are skipped")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if extra := flags.Args(); len(extra) > 0 {
		diagnostics.errorf("unexpected argument %q", extra[0])
		return 2
	}
	mode := output.ColorMode(*color)
	diagnostics = newDiagnosticWriter(stderr, mode, noColor)
	if *jsonOutput {
		diagnostics = newDiagnosticWriter(stderr, output.ColorNever, noColor)
	}
	if !mode.Valid() {
		diagnostics.errorf("invalid --color %q (want auto, always, or never)", *color)
		return 2
	}
	selectedLayer := usage.ToolLayer(*layer)
	if selectedLayer != usage.LayerEffective && selectedLayer != usage.LayerRuntime && selectedLayer != usage.LayerModel {
		diagnostics.errorf("invalid --layer %q (want effective, runtime, or model)", *layer)
		return 2
	}
	if kind != "tools" && *layer != string(usage.LayerEffective) {
		diagnostics.errorf("--layer is only valid for tools")
		return 2
	}
	selectedGroupBy := aggregate.SkillGroupBy(*groupBy)
	if !selectedGroupBy.Valid() {
		diagnostics.errorf("invalid --group-by %q (want turn or session)", *groupBy)
		return 2
	}
	if kind == "tools" && selectedGroupBy != aggregate.SkillGroupByTurn {
		diagnostics.errorf("--group-by is only valid for stats or skills")
		return 2
	}
	if kind != "skills" && *strict {
		diagnostics.errorf("--strict is only valid for skills")
		return 2
	}
	if kind != "skills" && *unused {
		_, _ = fmt.Fprintln(stderr, "error: --unused is only valid for skills")
		return 2
	}
	if len(roots) > 0 && (kind != "skills" || !*unused) {
		_, _ = fmt.Fprintln(stderr, "error: --root is only valid with --unused for skills")
		return 2
	}
	if *days < 0 {
		diagnostics.errorf("--days must be at least 1")
		return 2
	}
	daysSet := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "days" {
			daysSet = true
		}
	})
	if daysSet && *days == 0 {
		diagnostics.errorf("--days must be at least 1")
		return 2
	}

	home, err := codex.ResolveHome(*codexHome)
	if err != nil {
		diagnostics.errorf("resolve Codex home: %v", err)
		return 1
	}
	now := time.Now().UTC()
	input, err := codex.Load(home, codex.IngestOptions{Days: *days, DaysSet: daysSet, Now: now})
	if err != nil {
		diagnostics.errorf("read Codex history %q: %v", home, err)
		return 1
	}
	aggregateInput := aggregate.Input{Turns: input.Turns, SessionCount: len(input.Sessions), Warnings: input.Warnings}
	report := aggregate.Report{}
	warnings := input.Warnings
	var inventorySnapshot skillinventory.InventorySnapshot
	if *unused {
		userHome, err := os.UserHomeDir()
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "error: resolve user home for skill inventory: %v\n", err)
			return 1
		}
		resolvedRoots, err := skillinventory.ResolveRoots([]string(roots), userHome)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "error: resolve skill roots: %v\n", err)
			return 1
		}
		inventorySnapshot, err = skillinventory.Discover(skillinventory.DiscoverOptions{
			Roots:             resolvedRoots,
			AllowMissingRoots: len(roots) == 0,
		})
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "error: scan skill roots: %v\n", err)
			return 1
		}
		report = aggregate.BuildUnusedReport(aggregateInput, inventorySnapshot, *strict, selectedGroupBy)
		warnings = report.Warnings
	} else {
		report = aggregate.BuildOverviewBy(aggregateInput, selectedGroupBy)
		if kind == "tools" {
			report.Tools = aggregate.Tools(aggregateInput, selectedLayer)
		}
		if kind == "skills" {
			report.Skills = aggregate.SkillsBy(aggregateInput, *strict, selectedGroupBy)
		}
	}
	period := "all time"
	if daysSet {
		period = fmt.Sprintf("last %d days", *days)
	}
	context := output.ReportContext{Agent: "codex", Period: period, Layer: selectedLayer, SkillGroupBy: selectedGroupBy, Strict: *strict, ReferenceTime: now, Location: time.Local}
	if *unused {
		context.SkillView = output.SkillViewUnused
		context.SkillRoots = append([]string{}, inventorySnapshot.Roots...)
	}
	if *jsonOutput {
		if err := output.WriteJSON(stdout, kind, context, report); err != nil {
			diagnostics.errorf("render json: %v", err)
			return 1
		}
	} else {
		capabilities := output.TerminalCapabilities{ColorMode: mode, NoColor: noColor}
		if file, ok := stdout.(*os.File); ok {
			capabilities = output.DetectCapabilities(file, mode, capabilities.NoColor)
		}
		text := output.RenderHuman(kind, context, report, capabilities)
		if _, err := io.WriteString(stdout, text); err != nil {
			diagnostics.errorf("write report: %v", err)
			return 1
		}
	}
	writeWarnings(stderr, warnings, *verbose, diagnostics.capabilities)
	if *strictInput && len(warnings) > 0 {
		diagnostics.errorf("input warnings encountered (--strict-input)")
		return 1
	}
	return 0
}

type diagnosticWriter struct {
	w            io.Writer
	capabilities output.TerminalCapabilities
}

func newDiagnosticWriter(w io.Writer, mode output.ColorMode, noColor bool) diagnosticWriter {
	capabilities := output.TerminalCapabilities{ColorMode: mode, NoColor: noColor}
	if file, ok := w.(*os.File); ok {
		capabilities = output.DetectCapabilities(file, mode, noColor)
	}
	return diagnosticWriter{w: w, capabilities: capabilities}
}

func (d diagnosticWriter) errorf(format string, args ...any) {
	d.write("error", fmt.Sprintf(format, args...))
}

func (d diagnosticWriter) warningf(format string, args ...any) {
	d.write("warning", fmt.Sprintf(format, args...))
}

func (d diagnosticWriter) write(level, message string) {
	_, _ = fmt.Fprintf(d.w, "%s %s\n", output.DiagnosticPrefix(level, d.capabilities), message)
}

func writeWarnings(w io.Writer, warnings []usage.Warning, verbose bool, capabilities ...output.TerminalCapabilities) {
	if len(warnings) == 0 {
		return
	}
	diagnostics := diagnosticWriter{w: w, capabilities: output.TerminalCapabilities{ColorMode: output.ColorNever}}
	if len(capabilities) > 0 {
		diagnostics.capabilities = capabilities[0]
	}
	if !verbose {
		writeWarningSummary(diagnostics, warnings)
		return
	}
	for _, warning := range warnings {
		location := cleanWarningValue(warning.Path)
		if warning.Line > 0 {
			location = fmt.Sprintf("%s:%d", location, warning.Line)
		}
		typeSuffix := ""
		if warning.Type != "" {
			typeSuffix = " type=" + cleanWarningValue(warning.Type)
		}
		count := warning.Count
		if count <= 0 {
			count = 1
		}
		if location == "" {
			diagnostics.warningf("%s%s (%s)", cleanWarningValue(warning.Reason), typeSuffix, formatWarningCount(count))
		} else {
			diagnostics.warningf("%s%s at %s (%s)", cleanWarningValue(warning.Reason), typeSuffix, location, formatWarningCount(count))
		}
	}
}

type warningSummary struct {
	total     int
	records   int
	readFiles int
	files     map[string]struct{}
	reasons   map[string]int
	types     map[string]map[string]int
}

func writeWarningSummary(diagnostics diagnosticWriter, warnings []usage.Warning) {
	summary := summarizeWarnings(warnings)
	reasons := make([]string, 0, len(summary.reasons))
	for reason := range summary.reasons {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	details := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		typeCounts := summary.types[reason]
		if len(typeCounts) == 0 {
			details = append(details, cleanWarningValue(reason)+"="+formatWarningCount(summary.reasons[reason]))
			continue
		}
		types := make([]string, 0, len(typeCounts))
		for typ := range typeCounts {
			types = append(types, typ)
		}
		sort.Strings(types)
		items := make([]string, 0, len(types))
		for _, typ := range types {
			items = append(items, cleanWarningValue(typ)+"="+formatWarningCount(typeCounts[typ]))
		}
		details = append(details, cleanWarningValue(reason)+": "+strings.Join(items, ", "))
	}

	fileCount := len(summary.files)
	fileLabel := warningFileLabel(fileCount)
	detailText := strings.Join(details, ", ")
	switch {
	case summary.records > 0 && summary.readFiles > 0:
		diagnostics.warningf("skipped %s %s across %s %s; could not read %s %s (%s); use --verbose to show details", formatWarningCount(summary.records), warningRecordLabel(summary.records), formatWarningCount(fileCount), fileLabel, formatWarningCount(summary.readFiles), warningFileLabel(summary.readFiles), detailText)
	case summary.readFiles > 0:
		diagnostics.warningf("could not read %s %s (%s); use --verbose to show details", formatWarningCount(summary.readFiles), warningFileLabel(summary.readFiles), detailText)
	default:
		diagnostics.warningf("skipped %s %s across %s %s (%s); use --verbose to show details", formatWarningCount(summary.total), warningRecordLabel(summary.total), formatWarningCount(fileCount), fileLabel, detailText)
	}
}

func summarizeWarnings(warnings []usage.Warning) warningSummary {
	summary := warningSummary{files: make(map[string]struct{}), reasons: make(map[string]int), types: make(map[string]map[string]int)}
	for _, warning := range warnings {
		count := warning.Count
		if count <= 0 {
			count = 1
		}
		if warning.Reason == "read_file" {
			summary.readFiles += count
		} else {
			summary.total += count
			summary.records += count
		}
		if warning.Path != "" {
			summary.files[warning.Path] = struct{}{}
		}
		reason := strings.TrimSpace(warning.Reason)
		summary.reasons[reason] += count
		if typ := strings.TrimSpace(warning.Type); typ != "" {
			if summary.types[reason] == nil {
				summary.types[reason] = make(map[string]int)
			}
			summary.types[reason][typ] += count
		}
	}
	return summary
}

func warningRecordLabel(count int) string {
	if count == 1 {
		return "record"
	}
	return "records"
}

func warningFileLabel(count int) string {
	if count == 1 {
		return "file"
	}
	return "files"
}

func formatWarningCount(value int) string {
	if value < 0 {
		return "-" + formatWarningCount(-value)
	}
	text := strconv.Itoa(value)
	for i := len(text) - 3; i > 0; i -= 3 {
		text = text[:i] + "," + text[i:]
	}
	return text
}

func cleanWarningValue(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
}
