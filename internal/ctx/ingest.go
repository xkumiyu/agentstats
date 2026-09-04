// Package ctx reads ctx's public, read-only event stream.
package ctx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xkumiyu/agentstats/internal/usage"
)

const pageLimit = 10_000

// CommandResult is the result of one ctx invocation. The runner is injectable
// so tests can exercise the public stream contract without a real ctx store.
type CommandResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

type CommandRunner func(args []string) (CommandResult, error)

type IngestOptions struct {
	DataRoot string
	Days     int
	DaysSet  bool
	Now      time.Time
	Runner   CommandRunner
}

type IngestResult struct {
	Turns    []usage.Turn
	Sessions []SessionMetadata
	Agents   []string
	Warnings []usage.Warning
}

type SessionMetadata struct {
	ID                string          `json:"id"`
	Agent             string          `json:"agent"`
	Provider          string          `json:"provider,omitempty"`
	ProviderSessionID string          `json:"provider_session_id,omitempty"`
	CtxSessionID      string          `json:"ctx_session_id,omitempty"`
	Source            usage.SourceRef `json:"source"`
}

type event struct {
	Raw               map[string]any
	ID                string
	CtxSessionID      string
	Provider          string
	ProviderSessionID string
	EventType         string
	Role              string
	Timestamp         time.Time
	TimestampInvalid  bool
	Sequence          int64
	Ordinal           int64
	Line              int
}

type page struct {
	Events       []event
	NextCursor   string
	Terminal     bool
	Truncated    bool
	GenerationID string
	Complete     bool
	Warnings     []usage.Warning
}

// Load enumerates all ctx pages and converts the selected events into the
// common usage model. It never opens ctx storage itself.
func Load(dataRoot string, options IngestOptions) (IngestResult, error) {
	if strings.TrimSpace(options.DataRoot) == "" {
		options.DataRoot = dataRoot
	}
	runner := options.Runner
	if runner == nil {
		runner = runCommand
	}
	return load(options, runner)
}

func load(options IngestOptions, runner CommandRunner) (IngestResult, error) {
	now := options.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var cutoff time.Time
	if options.DaysSet || options.Days != 0 {
		if options.Days <= 0 {
			return IngestResult{}, errors.New("days must be at least 1")
		}
		cutoff = now.Add(-time.Duration(options.Days) * 24 * time.Hour)
	}

	var events []event
	warnings := make([]usage.Warning, 0)
	seenEvents := make(map[string]struct{})
	previousCursor := ""
	seenCursors := make(map[string]struct{})
	generation := ""
	for {
		args := []string{"list", "events", "--content", "full", "--format", "jsonl", "--quiet", "--limit", strconv.Itoa(pageLimit)}
		if strings.TrimSpace(options.DataRoot) != "" {
			args = append(args, "--data-root", options.DataRoot)
		}
		if !cutoff.IsZero() {
			args = append(args, "--since", cutoff.UTC().Truncate(time.Millisecond).Format(time.RFC3339Nano))
			args = append(args, "--until", now.UTC().Truncate(time.Millisecond).Format(time.RFC3339Nano))
		}
		if previousCursor != "" {
			args = append(args, "--cursor", previousCursor)
		}
		result, err := runner(args)
		if err != nil {
			return IngestResult{}, commandError(result, err)
		}
		if result.ExitCode != 0 {
			return IngestResult{}, commandError(result, fmt.Errorf("exit code %d", result.ExitCode))
		}
		current, err := parsePage(result.Stdout)
		if err != nil {
			return IngestResult{}, err
		}
		warnings = append(warnings, current.Warnings...)
		if current.GenerationID != "" {
			if generation != "" && generation != current.GenerationID {
				return IngestResult{}, fmt.Errorf("ctx event stream changed generation from %q to %q", generation, current.GenerationID)
			}
			generation = current.GenerationID
		}
		for _, item := range current.Events {
			if !accept(item.Timestamp, cutoff, cutoff.Add(time.Duration(options.Days)*24*time.Hour)) {
				continue
			}
			if item.ID != "" {
				if _, ok := seenEvents[item.ID]; ok {
					continue
				}
				seenEvents[item.ID] = struct{}{}
			}
			events = append(events, item)
		}
		if current.NextCursor == "" {
			if !current.Terminal || current.Truncated {
				return IngestResult{}, errors.New("ctx event stream ended without terminal completion")
			}
			break
		}
		if current.NextCursor == previousCursor {
			return IngestResult{}, errors.New("ctx event stream returned an unchanged cursor")
		}
		if _, ok := seenCursors[current.NextCursor]; ok {
			return IngestResult{}, errors.New("ctx event stream returned a repeated cursor")
		}
		seenCursors[current.NextCursor] = struct{}{}
		previousCursor = current.NextCursor
	}

	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Timestamp.Equal(events[j].Timestamp) {
			if events[i].Sequence == events[j].Sequence {
				if events[i].Ordinal == events[j].Ordinal {
					return events[i].ID < events[j].ID
				}
				return events[i].Ordinal < events[j].Ordinal
			}
			return events[i].Sequence < events[j].Sequence
		}
		if events[i].Timestamp.IsZero() {
			return false
		}
		if events[j].Timestamp.IsZero() {
			return true
		}
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	assembler := newAssembler(cutoff, warnings)
	for _, item := range events {
		assembler.consume(item)
	}
	assembler.flush()
	return assembler.result(), nil
}

func commandError(result CommandResult, err error) error {
	message := strings.TrimSpace(string(result.Stderr))
	if message != "" {
		return fmt.Errorf("ctx event enumeration: %w: %s", err, message)
	}
	return fmt.Errorf("ctx event enumeration: %w", err)
}

func runCommand(args []string) (CommandResult, error) {
	command := exec.Command("ctx", args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		}
	}
	return CommandResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: code}, err
}

func parsePage(data []byte) (page, error) {
	result := page{}
	reader := bufio.NewReader(bytes.NewReader(data))
	lineNo := 0
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			lineNo++
			var raw map[string]any
			if err := json.Unmarshal(bytes.TrimSpace(line), &raw); err != nil {
				result.Warnings = append(result.Warnings, warning("ctx_malformed_json", "", lineNo))
			} else {
				switch strings.ToLower(strings.TrimSpace(stringValue(raw, "record_type"))) {
				case "event_range_event":
					itemRaw := raw["event"]
					item, ok := itemRaw.(map[string]any)
					if !ok {
						result.Warnings = append(result.Warnings, warning("ctx_invalid_event", "", lineNo))
					} else {
						event := eventFromMap(item, lineNo)
						if event.TimestampInvalid {
							result.Warnings = append(result.Warnings, warning("ctx_invalid_timestamp", event.EventType, lineNo))
						}
						result.Events = append(result.Events, event)
					}
				case "event_range_completion":
					if result.Complete {
						return page{}, errors.New("ctx event stream returned multiple completion records")
					}
					result.Complete = true
					result.NextCursor = stringValue(raw, "next_cursor")
					result.Terminal = boolValue(raw, "terminal")
					result.Truncated = boolValue(raw, "truncated")
					result.GenerationID = stringValue(raw, "generation_id")
				default:
					result.Warnings = append(result.Warnings, warning("ctx_unknown_record", stringValue(raw, "record_type"), lineNo))
				}
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return page{}, fmt.Errorf("read ctx event page: %w", readErr)
		}
	}
	if !result.Complete {
		return page{}, errors.New("ctx event stream has no completion record")
	}
	return result, nil
}

func eventFromMap(raw map[string]any, line int) event {
	source := mapValue(raw, "source")
	provider := firstString(raw, "provider", "provider_name", "agent", "agent_name", "agent_id")
	if provider == "" {
		provider = firstString(source, "provider")
	}
	timestamp, timestampInvalid := eventTimestamp(raw)
	return event{
		Raw:               raw,
		ID:                firstString(raw, "ctx_event_id", "event_id"),
		CtxSessionID:      firstString(raw, "ctx_session_id", "session_uuid"),
		Provider:          provider,
		ProviderSessionID: firstString(raw, "provider_session_id", "provider_session", "session_id"),
		EventType:         firstString(raw, "event_type", "type"),
		Role:              strings.ToLower(strings.TrimSpace(firstString(raw, "role", "author"))),
		Timestamp:         timestamp,
		TimestampInvalid:  timestampInvalid,
		Sequence:          integerValue(raw, "sequence", "event_seq"),
		Ordinal:           integerValue(raw, "ordinal"),
		Line:              line,
	}
}

func eventTimestamp(raw map[string]any) (time.Time, bool) {
	if value := firstString(raw, "occurred_at", "timestamp", "time"); value != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return parsed, false
		}
		if value, ok := numberValue(raw["occurred_at_ms"]); ok && value != 0 {
			return time.UnixMilli(int64(value)), true
		}
		return time.Time{}, true
	}
	if value, ok := numberValue(raw["occurred_at_ms"]); ok && value != 0 {
		return time.UnixMilli(int64(value)), false
	}
	return time.Time{}, false
}

func accept(timestamp, cutoff, until time.Time) bool {
	if !cutoff.IsZero() && (timestamp.IsZero() || timestamp.Before(cutoff)) {
		return false
	}
	return until.IsZero() || timestamp.IsZero() || timestamp.Before(until)
}

func warning(reason, typ string, line int) usage.Warning {
	return usage.Warning{Reason: reason, Type: strings.TrimSpace(typ), Path: "ctx", Line: line, Count: 1}
}

type assembler struct {
	cutoff   time.Time
	warnings []usage.Warning
	turns    []usage.Turn
	current  map[string]*turnState
	ordinals map[string]int
	sessions map[string]SessionMetadata
	agents   map[string]struct{}
}

type turnState struct {
	turn       usage.Turn
	lastTime   time.Time
	explicitID bool
}

func newAssembler(cutoff time.Time, warnings []usage.Warning) *assembler {
	return &assembler{
		cutoff:   cutoff,
		warnings: append([]usage.Warning(nil), warnings...),
		current:  make(map[string]*turnState),
		ordinals: make(map[string]int),
		sessions: make(map[string]SessionMetadata),
		agents:   make(map[string]struct{}),
	}
}

func (a *assembler) consume(item event) {
	if isNonUsageEvent(item) {
		return
	}
	sessionID := item.sessionID()
	source := item.source()
	agent := source.Agent
	if !recognized(item) {
		if agent == "unknown" {
			a.warnings = append(a.warnings, warning("ctx_missing_agent", item.EventType, item.Line))
		}
		a.warnings = append(a.warnings, warning("ctx_unknown_event", item.EventType, item.Line))
		return
	}
	a.agents[agent] = struct{}{}
	if agent == "unknown" {
		a.warnings = append(a.warnings, warning("ctx_missing_agent", item.EventType, item.Line))
	}
	if _, ok := a.sessions[sessionID]; !ok {
		a.sessions[sessionID] = SessionMetadata{
			ID:                sessionID,
			Agent:             agent,
			Provider:          item.Provider,
			ProviderSessionID: item.ProviderSessionID,
			CtxSessionID:      item.CtxSessionID,
			Source:            source,
		}
	}
	turnID := item.turnID()
	if isUserMessage(item) {
		if current := a.current[sessionID]; current != nil && current.turn.UserPrompts > 0 && (turnID == "" || current.turn.ID != turnID) {
			a.finish(sessionID)
		}
	}
	current := a.ensure(sessionID, turnID, item)
	if !item.Timestamp.IsZero() {
		if current.turn.StartedAt.IsZero() || item.Timestamp.Before(current.turn.StartedAt) {
			current.turn.StartedAt = item.Timestamp
		}
		current.lastTime = item.Timestamp
	}
	a.observe(current, item)
	if isTurnComplete(item) {
		current.turn.EndedAt = item.Timestamp
		a.finish(sessionID)
	}
}

func (a *assembler) ensure(sessionID, explicitID string, item event) *turnState {
	if current := a.current[sessionID]; current != nil {
		if explicitID == "" || explicitID == current.turn.ID {
			return current
		}
		if !current.explicitID {
			current.adoptID(explicitID)
			return current
		}
		a.finish(sessionID)
	}
	a.ordinals[sessionID]++
	id := explicitID
	if id == "" {
		id = strconv.Itoa(a.ordinals[sessionID])
	}
	turn := usage.NewTurn(sessionID, id, a.ordinals[sessionID], item.source())
	a.current[sessionID] = &turnState{turn: turn, explicitID: explicitID != ""}
	return a.current[sessionID]
}

func (s *turnState) adoptID(id string) {
	if id == "" || s.turn.ID == id {
		return
	}
	s.turn.ID = id
	s.explicitID = true
	for i := range s.turn.ModelTools {
		s.turn.ModelTools[i].TurnID = id
	}
	for i := range s.turn.RuntimeTools {
		s.turn.RuntimeTools[i].TurnID = id
	}
	for i := range s.turn.SkillEvidence {
		s.turn.SkillEvidence[i].TurnID = id
	}
}

func (a *assembler) finish(sessionID string) {
	current := a.current[sessionID]
	if current == nil {
		return
	}
	if current.turn.EndedAt.IsZero() {
		current.turn.EndedAt = current.lastTime
	}
	a.turns = append(a.turns, current.turn)
	delete(a.current, sessionID)
}

func (a *assembler) flush() {
	ids := make([]string, 0, len(a.current))
	for id := range a.current {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		a.finish(id)
	}
	sort.SliceStable(a.turns, func(i, j int) bool {
		if a.turns[i].StartedAt.Equal(a.turns[j].StartedAt) {
			if a.turns[i].SessionID == a.turns[j].SessionID {
				return a.turns[i].Ordinal < a.turns[j].Ordinal
			}
			return a.turns[i].SessionID < a.turns[j].SessionID
		}
		return a.turns[i].StartedAt.Before(a.turns[j].StartedAt)
	})
}

func (a *assembler) result() IngestResult {
	agents := make([]string, 0, len(a.agents))
	for agent := range a.agents {
		agents = append(agents, agent)
	}
	sort.Strings(agents)
	sessionIDs := make([]string, 0, len(a.sessions))
	for id := range a.sessions {
		sessionIDs = append(sessionIDs, id)
	}
	sort.Strings(sessionIDs)
	sessions := make([]SessionMetadata, 0, len(sessionIDs))
	for _, id := range sessionIDs {
		sessions = append(sessions, a.sessions[id])
	}
	return IngestResult{Turns: a.turns, Sessions: sessions, Agents: agents, Warnings: a.warnings}
}

func (a *assembler) observe(current *turnState, item event) {
	text := eventText(item.Raw)
	if isUserMessage(item) {
		injected := onlyInjectedSkill(text)
		if !injected {
			current.turn.UserPrompts++
		}
		current.turn.SkillEvidence = append(current.turn.SkillEvidence, usage.DetectExplicitRequest(text, current.turn.SessionID, current.turn.ID, item.Timestamp, item.source())...)
		if hasSelectedSkillInstructions(item.Raw) {
			for _, skillText := range eventSkillTexts(item.Raw) {
				current.turn.SkillEvidence = append(current.turn.SkillEvidence, usage.DetectSelectedSkillInstructions(skillText, current.turn.SessionID, current.turn.ID, item.Timestamp, item.source())...)
			}
		} else {
			for _, skillText := range eventSkillTexts(item.Raw) {
				current.turn.SkillEvidence = append(current.turn.SkillEvidence, usage.DetectInjectedSkillsWithMode(skillText, current.turn.SessionID, current.turn.ID, usage.ModeUnknown, item.Timestamp, item.source())...)
			}
		}
		for _, value := range eventPayloadValues(item.Raw) {
			current.turn.SkillEvidence = append(current.turn.SkillEvidence, usage.DetectRuntimeSkillItems(value, current.turn.SessionID, current.turn.ID, item.Timestamp, item.source())...)
		}
	}

	tools := eventTools(item, current.turn.SessionID, current.turn.ID)
	seen := make(map[string]struct{}, len(tools))
	for _, observation := range tools {
		key := string(observation.Layer) + "\x00" + observation.CallID + "\x00" + observation.ItemID + "\x00" + observation.CanonicalName
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if observation.Layer == usage.LayerRuntime {
			current.turn.RuntimeTools = append(current.turn.RuntimeTools, observation)
		} else {
			current.turn.ModelTools = append(current.turn.ModelTools, observation)
		}
		current.turn.SkillEvidence = append(current.turn.SkillEvidence, usage.DetectStructuredSkillTool(observation)...)
		if observation.Layer == usage.LayerRuntime && observation.Status != usage.StatusFailure {
			current.turn.SkillEvidence = append(current.turn.SkillEvidence, usage.DetectImplicitAccess(observation.Arguments, current.turn.SessionID, current.turn.ID, item.Timestamp, item.source())...)
		} else if observation.Layer == usage.LayerModel && observation.Status == usage.StatusSuccess && isExecTool(observation) {
			for _, command := range execCommandsFromModelArguments(observation.Arguments) {
				current.turn.SkillEvidence = append(current.turn.SkillEvidence, usage.DetectImplicitAccess(command, current.turn.SessionID, current.turn.ID, item.Timestamp, item.source())...)
			}
		}
	}
	if strings.Contains(compact(item.EventType), "skill") {
		for _, name := range skillActivityNames(item.Raw) {
			current.turn.SkillEvidence = append(current.turn.SkillEvidence, usage.NewSkillEvidence(current.turn.SessionID, current.turn.ID, name, usage.ModeExplicit, usage.MethodStructuredTool, usage.StateConfirmed, item.Timestamp, item.source()))
		}
	}
}

func eventTools(item event, sessionID, turnID string) []usage.ToolObservation {
	result := make([]usage.ToolObservation, 0)
	seen := make(map[string]struct{})
	for _, value := range eventPayloadValues(item.Raw) {
		collectActivityTools(value, item, sessionID, turnID, &result, seen)
	}
	if isToolEvent(item) {
		if observation, ok := normalizeToolMap(item.Raw, item, nil, sessionID, turnID); ok {
			result = append(result, observation)
		}
	}
	return result
}

func isExecTool(observation usage.ToolObservation) bool {
	return strings.EqualFold(observation.RawName, "exec") || strings.EqualFold(observation.CanonicalName, "exec")
}

// execCommandsFromModelArguments extracts structured commands from a
// completed Codex exec payload. ctx may preserve the provider-native input as
// a JSON object or as JavaScript that calls tools.exec_command; arbitrary text
// is deliberately not parsed as a command.
func execCommandsFromModelArguments(arguments string) []string {
	arguments = strings.TrimSpace(arguments)
	commands := make([]string, 0, 1)
	seen := make(map[string]struct{})
	appendCommand := func(command string) {
		command = strings.TrimSpace(command)
		if command == "" {
			return
		}
		if _, ok := seen[command]; ok {
			return
		}
		seen[command] = struct{}{}
		commands = append(commands, command)
	}

	var payload map[string]any
	if err := json.NewDecoder(strings.NewReader(arguments)).Decode(&payload); err == nil {
		appendCommand(stringValue(payload, "cmd"))
		appendCommand(stringValue(payload, "command"))
	}
	for _, marker := range []string{"exec_command(", "execCommand("} {
		for offset := 0; offset < len(arguments); {
			relative := strings.Index(arguments[offset:], marker)
			if relative < 0 {
				break
			}
			markerStart := offset + relative
			index := markerStart + len(marker)
			candidate := arguments[index:]
			object, ok := javascriptObjectArgument(candidate)
			if ok {
				if javascriptObjectHasShorthand(object) {
					for _, literal := range javascriptMappedCommandLiterals(arguments, markerStart) {
						appendCommand(literal)
					}
				}
				appendCommand(javascriptObjectCommand(object))
			}
			offset = index
		}
	}
	if len(commands) == 0 {
		appendCommand(javascriptObjectCommand(arguments))
	}
	return commands
}

func javascriptObjectArgument(source string) (string, bool) {
	start := 0
	for start < len(source) && (source[start] == ' ' || source[start] == '\t' || source[start] == '\r' || source[start] == '\n') {
		start++
	}
	if start >= len(source) || source[start] != '{' {
		return "", false
	}
	depth := 0
	for index := start; index < len(source); index++ {
		switch source[index] {
		case '\'', '"', '`':
			_, next, ok := javascriptString(source, index)
			if !ok {
				return "", false
			}
			index = next - 1
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return source[start : index+1], true
			}
		}
	}
	return "", false
}

func javascriptObjectCommand(source string) string {
	for index := 0; index < len(source); {
		switch source[index] {
		case '\'', '"', '`':
			key, next, ok := javascriptString(source, index)
			if !ok {
				return ""
			}
			if key == "cmd" || key == "command" {
				if command, ok := javascriptPropertyString(source, next); ok {
					return command
				}
			}
			index = next
			continue
		}
		if isJavaScriptIdentifierStart(source[index]) {
			start := index
			index++
			for index < len(source) && isJavaScriptIdentifierPart(source[index]) {
				index++
			}
			key := source[start:index]
			if key == "cmd" || key == "command" {
				if command, ok := javascriptPropertyString(source, index); ok {
					return command
				}
			}
			continue
		}
		index++
	}
	return ""
}

func javascriptObjectHasShorthand(source string) bool {
	for index := 0; index < len(source); {
		switch source[index] {
		case '\'', '"', '`':
			_, next, ok := javascriptString(source, index)
			if !ok {
				return false
			}
			index = next
			continue
		}
		if !isJavaScriptIdentifierStart(source[index]) {
			index++
			continue
		}
		start := index
		index++
		for index < len(source) && isJavaScriptIdentifierPart(source[index]) {
			index++
		}
		key := source[start:index]
		if key != "cmd" && key != "command" {
			continue
		}
		for index < len(source) && (source[index] == ' ' || source[index] == '\t' || source[index] == '\r' || source[index] == '\n') {
			index++
		}
		if index < len(source) && (source[index] == ',' || source[index] == '}') {
			return true
		}
	}
	return false
}

func javascriptMappedCommandLiterals(source string, callIndex int) []string {
	if callIndex < 0 || callIndex > len(source) {
		return nil
	}
	before := source[:callIndex]
	mapIndex := strings.LastIndex(before, ".map")
	if mapIndex < 0 {
		return nil
	}
	variable := javascriptIdentifierBefore(before, mapIndex)
	if variable == "" {
		return nil
	}
	declaration := -1
	for _, prefix := range []string{"const ", "let ", "var "} {
		if index := strings.LastIndex(before, prefix+variable); index > declaration {
			declaration = index + len(prefix+variable)
		}
	}
	if declaration < 0 {
		return nil
	}
	index := declaration
	for index < len(before) && (before[index] == ' ' || before[index] == '\t' || before[index] == '\r' || before[index] == '\n') {
		index++
	}
	if index >= len(before) || before[index] != '=' {
		return nil
	}
	index++
	for index < len(before) && (before[index] == ' ' || before[index] == '\t' || before[index] == '\r' || before[index] == '\n') {
		index++
	}
	if index >= len(before) || before[index] != '[' {
		return nil
	}
	array, ok := javascriptArrayArgument(before[index:])
	if !ok {
		return nil
	}
	return javascriptStringLiterals(array)
}

func javascriptIdentifierBefore(source string, end int) string {
	for end > 0 && (source[end-1] == ' ' || source[end-1] == '\t' || source[end-1] == '\r' || source[end-1] == '\n') {
		end--
	}
	start := end
	for start > 0 && isJavaScriptIdentifierPart(source[start-1]) {
		start--
	}
	return source[start:end]
}

func javascriptArrayArgument(source string) (string, bool) {
	start := 0
	for start < len(source) && (source[start] == ' ' || source[start] == '\t' || source[start] == '\r' || source[start] == '\n') {
		start++
	}
	if start >= len(source) || source[start] != '[' {
		return "", false
	}
	depth := 0
	for index := start; index < len(source); index++ {
		switch source[index] {
		case '\'', '"', '`':
			_, next, ok := javascriptString(source, index)
			if !ok {
				return "", false
			}
			index = next - 1
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return source[start : index+1], true
			}
		}
	}
	return "", false
}

func javascriptStringLiterals(source string) []string {
	result := make([]string, 0)
	for index := 0; index < len(source); {
		if source[index] != '\'' && source[index] != '"' && source[index] != '`' {
			index++
			continue
		}
		value, next, ok := javascriptString(source, index)
		if !ok {
			break
		}
		result = append(result, value)
		index = next
	}
	return result
}

func javascriptPropertyString(source string, index int) (string, bool) {
	for index < len(source) && (source[index] == ' ' || source[index] == '\t' || source[index] == '\r' || source[index] == '\n') {
		index++
	}
	if index >= len(source) || source[index] != ':' {
		return "", false
	}
	index++
	for index < len(source) && (source[index] == ' ' || source[index] == '\t' || source[index] == '\r' || source[index] == '\n') {
		index++
	}
	if index >= len(source) || (source[index] != '\'' && source[index] != '"' && source[index] != '`') {
		return "", false
	}
	value, _, ok := javascriptString(source, index)
	return value, ok
}

func javascriptString(source string, start int) (string, int, bool) {
	quote := source[start]
	var value strings.Builder
	for index := start + 1; index < len(source); index++ {
		switch source[index] {
		case quote:
			return value.String(), index + 1, true
		case '\\':
			if index+1 >= len(source) {
				return "", 0, false
			}
			index++
			switch source[index] {
			case 'n':
				value.WriteByte('\n')
			case 'r':
				value.WriteByte('\r')
			case 't':
				value.WriteByte('\t')
			case 'b':
				value.WriteByte('\b')
			case 'f':
				value.WriteByte('\f')
			case 'v':
				value.WriteByte('\v')
			case '0':
				value.WriteByte('\x00')
			case 'x':
				if index+2 >= len(source) {
					return "", 0, false
				}
				parsed, err := strconv.ParseUint(source[index+1:index+3], 16, 8)
				if err != nil {
					return "", 0, false
				}
				value.WriteByte(byte(parsed))
				index += 2
			case 'u':
				if index+4 >= len(source) {
					return "", 0, false
				}
				parsed, err := strconv.ParseUint(source[index+1:index+5], 16, 16)
				if err != nil {
					return "", 0, false
				}
				value.WriteRune(rune(parsed))
				index += 4
			default:
				value.WriteByte(source[index])
			}
		default:
			value.WriteByte(source[index])
		}
	}
	return "", 0, false
}

func isJavaScriptIdentifierStart(value byte) bool {
	return value == '_' || value == '$' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func isJavaScriptIdentifierPart(value byte) bool {
	return isJavaScriptIdentifierStart(value) || value >= '0' && value <= '9'
}

func collectActivityTools(value any, item event, sessionID, turnID string, result *[]usage.ToolObservation, seen map[string]struct{}) {
	switch typed := value.(type) {
	case []any:
		for _, child := range typed {
			collectActivityTools(child, item, sessionID, turnID, result, seen)
		}
	case map[string]any:
		if invocation, ok := typed["invocation"].(map[string]any); ok {
			merged := copyMap(invocation)
			if completion, ok := typed["result"].(map[string]any); ok {
				mergeMap(merged, completion)
			}
			if observation, ok := normalizeToolMap(merged, item, typed, sessionID, turnID); ok {
				appendUniqueTool(result, seen, observation)
			}
		}
		if isToolLikeMap(typed) {
			if observation, ok := normalizeToolMap(typed, item, nil, sessionID, turnID); ok {
				appendUniqueTool(result, seen, observation)
			}
		}
		for key, child := range typed {
			if key == "result" || key == "invocation" {
				continue
			}
			collectActivityTools(child, item, sessionID, turnID, result, seen)
		}
	}
}

func appendUniqueTool(result *[]usage.ToolObservation, seen map[string]struct{}, observation usage.ToolObservation) {
	key := string(observation.Layer) + "\x00" + observation.CallID + "\x00" + observation.ItemID + "\x00" + observation.CanonicalName
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	*result = append(*result, observation)
}

func normalizeToolMap(raw map[string]any, item event, parent map[string]any, sessionID, turnID string) (usage.ToolObservation, bool) {
	value := copyMap(raw)
	kind := compact(item.EventType)
	valueKind := compact(firstString(value, "type", "kind", "activity_type"))
	layer := usage.LayerModel
	if runtimeEventKind(kind) || runtimeEventKind(valueKind) {
		layer = usage.LayerRuntime
	}
	if requested := strings.ToLower(strings.TrimSpace(firstString(value, "layer"))); requested == string(usage.LayerModel) || requested == string(usage.LayerRuntime) {
		layer = usage.ToolLayer(requested)
	}
	protocol := strings.ToLower(strings.TrimSpace(firstString(value, "protocol", "transport")))
	if layer == usage.LayerRuntime {
		if stringValue(value, "type") == "" {
			switch protocol {
			case "mcp":
				value["type"] = "McpToolCall"
			case "command", "shell":
				value["type"] = "CommandExecution"
			}
		}
		if parent != nil {
			mergeMap(value, mapValue(parent, "result"))
		}
		if arguments := mapValue(value, "arguments"); arguments != nil {
			if command := firstString(arguments, "command", "cmd", "script"); command != "" {
				value["command"] = command
			}
		}
		return usage.NormalizeRuntimeItem(sessionID, turnID, value, item.Timestamp, item.source())
	}
	if stringValue(value, "type") == "" {
		value["type"] = "custom_tool_call"
	}
	return usage.NormalizeModelCall(sessionID, turnID, value, item.Timestamp, item.source())
}

func runtimeEventKind(kind string) bool {
	if strings.Contains(kind, "completion") || strings.Contains(kind, "result") || strings.Contains(kind, "runtime") || strings.Contains(kind, "execution") {
		return true
	}
	switch {
	case kind == "command", strings.Contains(kind, "shellcommand"):
		return true
	case strings.Contains(kind, "mcptoolcall"), strings.Contains(kind, "mcpcall"):
		return true
	case strings.Contains(kind, "filechange"), strings.Contains(kind, "websearch"):
		return true
	case strings.Contains(kind, "imageview"), strings.Contains(kind, "imagegeneration"):
		return true
	case strings.Contains(kind, "collabagenttoolcall"), strings.Contains(kind, "collaboration"):
		return true
	default:
		return false
	}
}

func isToolLikeMap(value map[string]any) bool {
	kind := compact(firstString(value, "type", "kind", "activity_type"))
	if strings.Contains(kind, "tool") || strings.Contains(kind, "command") || strings.Contains(kind, "execution") || strings.Contains(kind, "invocation") || runtimeEventKind(kind) {
		return true
	}
	return firstString(value, "raw_name", "tool_name") != ""
}

func isToolEvent(item event) bool {
	kind := compact(item.EventType)
	switch kind {
	case "toolcall", "tooluse", "toolresult", "tooloutput", "toolinvocation", "command", "commandcompletion", "commandexecution", "shellcommand", "mcpcall", "mcptoolcall", "customtoolcall", "functioncall", "invocation", "runtime", "runtimeactivity", "itemcompleted":
		return true
	default:
		return false
	}
}

func skillActivityNames(raw map[string]any) []string {
	result := make(map[string]struct{})
	var visit func(any)
	visit = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			kind := compact(firstString(typed, "type", "kind", "activity_type"))
			if kind == "skill" || strings.Contains(kind, "skilltool") {
				for _, key := range []string{"name", "skill", "skill_name", "skillName", "path"} {
					if name := skillName(stringValue(typed, key)); name != "" {
						result[name] = struct{}{}
					}
				}
			}
			for _, child := range typed {
				visit(child)
			}
		case []any:
			for _, child := range typed {
				visit(child)
			}
		}
	}
	for _, value := range eventPayloadValues(raw) {
		visit(value)
	}
	names := make([]string, 0, len(result))
	for name := range result {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func skillName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.TrimPrefix(value, "$")
	if strings.Contains(value, "/") {
		return usage.SkillNameFromPath(value)
	}
	for _, r := range value {
		if r == '-' || r == '_' || r == '.' || r == ':' ||
			r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			continue
		}
		return ""
	}
	return value
}

func recognized(item event) bool {
	kind := compact(item.EventType)
	if isUserMessage(item) || isToolEvent(item) {
		return true
	}
	switch kind {
	case "message", "usermessage", "userinput", "prompt", "assistantmessage", "activity", "skill", "skillactivity", "skilltool", "skillinvocation", "turnstarted", "turncomplete", "taskstarted", "taskcomplete", "turnaborted":
		return true
	}
	return false
}

func isUserMessage(item event) bool {
	kind := compact(item.EventType)
	return item.Role == "user" && (kind == "message" || kind == "usermessage" || kind == "userinput" || kind == "prompt" || kind == "")
}

func isTurnComplete(item event) bool {
	kind := compact(item.EventType)
	return kind == "turncomplete" || kind == "taskcomplete" || kind == "turnaborted"
}

func isNonUsageEvent(item event) bool {
	// ctx summaries are provider metadata, not a user prompt or tool
	// observation. They are expected in a complete event stream and should not
	// produce an unknown-event warning or an empty usage turn.
	return compact(item.EventType) == "summary"
}

func (item event) source() usage.SourceRef {
	path := "ctx"
	if item.ID != "" {
		path = "ctx://" + item.ID
	}
	source := usage.NewCtxSourceRef(path, item.Provider, item.ProviderSessionID, item.CtxSessionID, item.ID)
	source.Line = item.Line
	return source
}

func (item event) sessionID() string {
	source := item.source()
	parts := []string{"ctx", source.Agent, item.Provider, item.ProviderSessionID, item.CtxSessionID}
	if item.ProviderSessionID == "" && item.CtxSessionID == "" {
		parts = append(parts, item.ID)
	}
	return strings.Join(parts, "\x00")
}

func (item event) turnID() string {
	for _, value := range eventPayloadValues(item.Raw) {
		if payload, ok := value.(map[string]any); ok {
			if id := findString(payload, "turn_id", "turnId", "provider_turn_id", "providerTurnId", "task_id", "taskId", "interaction_id", "interactionId"); id != "" {
				return id
			}
		}
	}
	return ""
}

func eventText(raw map[string]any) string {
	for _, value := range eventPayloadValues(raw) {
		payload, ok := value.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"text", "message", "content"} {
			if text := textValue(payload[key]); text != "" {
				return text
			}
		}
	}
	return ""
}

func eventSkillTexts(raw map[string]any) []string {
	texts := make([]string, 0)
	seen := make(map[string]struct{})
	for _, value := range eventPayloadValues(raw) {
		payload, ok := value.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"text", "message", "content", "structured_content"} {
			text := textValue(payload[key])
			if text == "" {
				continue
			}
			if _, ok := seen[text]; ok {
				continue
			}
			seen[text] = struct{}{}
			texts = append(texts, text)
		}
	}
	return texts
}

func hasSelectedSkillInstructions(raw map[string]any) bool {
	for _, value := range eventPayloadValues(raw) {
		if containsSelectedSkillInstructions(value) {
			return true
		}
	}
	return false
}

func containsSelectedSkillInstructions(value any) bool {
	switch typed := value.(type) {
	case []any:
		for _, child := range typed {
			if containsSelectedSkillInstructions(child) {
				return true
			}
		}
	case map[string]any:
		if kinds, ok := typed["content_item_kinds"]; ok && selectedSkillKind(kinds) {
			return true
		}
		for _, child := range typed {
			if containsSelectedSkillInstructions(child) {
				return true
			}
		}
	}
	return false
}

func selectedSkillKind(value any) bool {
	switch typed := value.(type) {
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "skills.selected_skill_instructions")
	case []any:
		for _, child := range typed {
			if selectedSkillKind(child) {
				return true
			}
		}
	}
	return false
}

func textValue(value any) string {
	text := valueText(value)
	if text == "" {
		return ""
	}
	var decoded any
	if err := json.Unmarshal([]byte(text), &decoded); err == nil {
		if decodedText := valueText(decoded); decodedText != "" {
			return decodedText
		}
	}
	return text
}

func onlyInjectedSkill(text string) bool {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(strings.ToLower(text), "<skill") {
		return false
	}
	if len(usage.DetectInjectedSkills(text, "", "", time.Time{}, usage.SourceRef{})) == 0 {
		return false
	}
	lower := strings.ToLower(text)
	closeIndex := strings.LastIndex(lower, "</skill")
	if closeIndex < 0 {
		return false
	}
	end := strings.Index(lower[closeIndex:], ">")
	return end >= 0 && strings.TrimSpace(text[closeIndex+end+1:]) == ""
}

func findString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if text := stringValue(value, key); text != "" {
			return text
		}
	}
	for key, child := range value {
		if key == "content" || key == "activity" || key == "structured_content" || key == "invocation" || key == "metadata" || key == "source" || key == "item" || key == "internal_chat_message_metadata_passthrough" {
			if nested := mapValueFromAny(child); nested != nil {
				if text := findString(nested, keys...); text != "" {
					return text
				}
			}
		}
	}
	return ""
}

func valueText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, child := range typed {
			if text := valueText(child); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		for _, key := range []string{"text", "value", "message", "content", "input", "source"} {
			if text := valueText(typed[key]); text != "" {
				return text
			}
		}
	}
	return ""
}

// eventPayloadValues returns the event itself, the normalized content maps,
// and JSON payloads embedded in ctx's source projection. ctx keeps provider
// payloads under event.text or source.text when full content is requested;
// older synthetic projections may put them under content.source.text.
// Exposing all three shapes lets the normalizer consume provider payloads
// without reading provider-owned files.
func eventPayloadValues(raw map[string]any) []any {
	values := []any{raw}
	if text := stringValue(raw, "text"); text != "" {
		var decoded any
		if err := json.Unmarshal([]byte(text), &decoded); err == nil {
			values = append(values, decoded)
		}
	}
	if source := mapValue(raw, "source"); source != nil {
		values = append(values, source)
		if text := stringValue(source, "text"); text != "" {
			var decoded any
			if err := json.Unmarshal([]byte(text), &decoded); err == nil {
				values = append(values, decoded)
			}
		}
	}
	content := mapValue(raw, "content")
	if content == nil {
		return values
	}
	values = append(values, content)
	source := mapValue(content, "source")
	if source == nil {
		return values
	}
	values = append(values, source)
	text := stringValue(source, "text")
	if text == "" {
		return values
	}
	var decoded any
	if err := json.Unmarshal([]byte(text), &decoded); err == nil {
		values = append(values, decoded)
	}
	return values
}

func mapValue(raw map[string]any, key string) map[string]any {
	return mapValueFromAny(raw[key])
}

func mapValueFromAny(value any) map[string]any {
	valueMap, _ := value.(map[string]any)
	return valueMap
}

func copyMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, child := range value {
		result[key] = child
	}
	return result
}

func mergeMap(target, source map[string]any) {
	for key, value := range source {
		target[key] = value
	}
}

func firstString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if text := stringValue(value, key); text != "" {
			return text
		}
	}
	return ""
}

func stringValue(value map[string]any, key string) string {
	text, _ := value[key].(string)
	return strings.TrimSpace(text)
}

func boolValue(value map[string]any, key string) bool {
	result, _ := value[key].(bool)
	return result
}

func integerValue(value map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if number, ok := numberValue(value[key]); ok {
			return int64(number)
		}
	}
	return 0
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func compact(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, " ", "")
	return value
}
