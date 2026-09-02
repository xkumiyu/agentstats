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

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
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
		_, _ = fmt.Fprintf(stderr, "error: unknown command %q\n\n%s", args[0], usageText)
		return 2
	}
	if hasOption(args[1:], "--help") || hasOption(args[1:], "-h") {
		_, _ = io.WriteString(stdout, commandUsage(kind))
		return 0
	}
	if hasOption(args[1:], "--version") {
		_, _ = fmt.Fprintln(stderr, "error: --version is a top-level option; use agentstats --version")
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
		_, _ = fmt.Fprintf(stderr, "error: unexpected argument %q\n", extra[0])
		return 2
	}
	mode := output.ColorMode(*color)
	if !mode.Valid() {
		_, _ = fmt.Fprintf(stderr, "error: invalid --color %q (want auto, always, or never)\n", *color)
		return 2
	}
	selectedLayer := usage.ToolLayer(*layer)
	if selectedLayer != usage.LayerEffective && selectedLayer != usage.LayerRuntime && selectedLayer != usage.LayerModel {
		_, _ = fmt.Fprintf(stderr, "error: invalid --layer %q (want effective, runtime, or model)\n", *layer)
		return 2
	}
	if kind != "tools" && *layer != string(usage.LayerEffective) {
		_, _ = fmt.Fprintln(stderr, "error: --layer is only valid for tools")
		return 2
	}
	selectedGroupBy := aggregate.SkillGroupBy(*groupBy)
	if !selectedGroupBy.Valid() {
		_, _ = fmt.Fprintf(stderr, "error: invalid --group-by %q (want turn or session)\n", *groupBy)
		return 2
	}
	if kind == "tools" && selectedGroupBy != aggregate.SkillGroupByTurn {
		_, _ = fmt.Fprintln(stderr, "error: --group-by is only valid for stats or skills")
		return 2
	}
	if kind != "skills" && *strict {
		_, _ = fmt.Fprintln(stderr, "error: --strict is only valid for skills")
		return 2
	}
	if *days < 0 {
		_, _ = fmt.Fprintln(stderr, "error: --days must be at least 1")
		return 2
	}
	daysSet := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "days" {
			daysSet = true
		}
	})
	if daysSet && *days == 0 {
		_, _ = fmt.Fprintln(stderr, "error: --days must be at least 1")
		return 2
	}

	home, err := codex.ResolveHome(*codexHome)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: resolve Codex home: %v\n", err)
		return 1
	}
	now := time.Now().UTC()
	input, err := codex.Load(home, codex.IngestOptions{Days: *days, DaysSet: daysSet, Now: now})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "error: read Codex history %q: %v\n", home, err)
		return 1
	}
	aggregateInput := aggregate.Input{Turns: input.Turns, SessionCount: len(input.Sessions), Warnings: input.Warnings}
	report := aggregate.BuildOverviewBy(aggregateInput, selectedGroupBy)
	if kind == "tools" {
		report.Tools = aggregate.Tools(aggregateInput, selectedLayer)
	}
	if kind == "skills" {
		report.Skills = aggregate.SkillsBy(aggregateInput, *strict, selectedGroupBy)
	}
	period := "all time"
	if daysSet {
		period = fmt.Sprintf("last %d days", *days)
	}
	context := output.ReportContext{Agent: "codex", Period: period, Layer: selectedLayer, SkillGroupBy: selectedGroupBy, Strict: *strict, ReferenceTime: now, Location: time.Local}
	if *jsonOutput {
		if err := output.WriteJSON(stdout, kind, context, report); err != nil {
			_, _ = fmt.Fprintf(stderr, "error: render json: %v\n", err)
			return 1
		}
	} else {
		_, noColor := os.LookupEnv("NO_COLOR")
		capabilities := output.TerminalCapabilities{ColorMode: mode, NoColor: noColor}
		if file, ok := stdout.(*os.File); ok {
			capabilities = output.DetectCapabilities(file, mode, capabilities.NoColor)
		}
		text := output.RenderHuman(kind, context, report, capabilities)
		if _, err := io.WriteString(stdout, text); err != nil {
			_, _ = fmt.Fprintf(stderr, "error: write report: %v\n", err)
			return 1
		}
	}
	writeWarnings(stderr, input.Warnings, *verbose)
	if *strictInput && len(input.Warnings) > 0 {
		_, _ = fmt.Fprintln(stderr, "error: input warnings encountered (--strict-input)")
		return 1
	}
	return 0
}

func writeWarnings(w io.Writer, warnings []usage.Warning, verbose ...bool) {
	if len(warnings) == 0 {
		return
	}
	showDetails := len(verbose) > 0 && verbose[0]
	if !showDetails {
		writeWarningSummary(w, warnings)
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
			_, _ = fmt.Fprintf(w, "warning: %s%s (%s)\n", cleanWarningValue(warning.Reason), typeSuffix, formatWarningCount(count))
		} else {
			_, _ = fmt.Fprintf(w, "warning: %s%s at %s (%s)\n", cleanWarningValue(warning.Reason), typeSuffix, location, formatWarningCount(count))
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

func writeWarningSummary(w io.Writer, warnings []usage.Warning) {
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
		_, _ = fmt.Fprintf(w, "warning: skipped %s %s across %s %s; could not read %s %s (%s); use --verbose to show details\n", formatWarningCount(summary.records), warningRecordLabel(summary.records), formatWarningCount(fileCount), fileLabel, formatWarningCount(summary.readFiles), warningFileLabel(summary.readFiles), detailText)
	case summary.readFiles > 0:
		_, _ = fmt.Fprintf(w, "warning: could not read %s %s (%s); use --verbose to show details\n", formatWarningCount(summary.readFiles), warningFileLabel(summary.readFiles), detailText)
	default:
		_, _ = fmt.Fprintf(w, "warning: skipped %s %s across %s %s (%s); use --verbose to show details\n", formatWarningCount(summary.total), warningRecordLabel(summary.total), formatWarningCount(fileCount), fileLabel, detailText)
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
