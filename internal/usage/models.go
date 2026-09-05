package usage

import (
	"strings"
	"time"
	"unicode"
)

// SourceKind identifies the history source selected for an invocation.
type SourceKind string

const (
	SourceCodex SourceKind = "codex"
	SourceCtx   SourceKind = "ctx"
)

func (s SourceKind) Valid() bool { return s == SourceCodex || s == SourceCtx }

// CanonicalAgentID returns the stable, lower-case identifier used for
// cross-agent identity and sorting. Provider names are intentionally kept
// recognizable instead of being mapped to a closed list.
func CanonicalAgentID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "unknown"
	}
	var result strings.Builder
	separator := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if separator && result.Len() > 0 {
				result.WriteByte('-')
			}
			result.WriteRune(r)
			separator = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			separator = result.Len() > 0
		}
	}
	if result.Len() == 0 {
		return "unknown"
	}
	return result.String()
}

// AgentDisplayName returns the stable human-facing name for a canonical ID.
func AgentDisplayName(value string) string {
	id := CanonicalAgentID(value)
	switch id {
	case "codex":
		return "Codex"
	case "opencode":
		return "OpenCode"
	case "claude-code":
		return "Claude Code"
	case "github-copilot":
		return "GitHub Copilot"
	case "unknown":
		return "unknown"
	}
	parts := strings.Split(id, "-")
	for i, part := range parts {
		if part == "" {
			continue
		}
		runes := []rune(part)
		parts[i] = strings.ToUpper(string(runes[0])) + string(runes[1:])
	}
	return strings.Join(parts, " ")
}

// SourceRef identifies the immutable source position of an observation.
type SourceRef struct {
	Path              string     `json:"path"`
	Line              int        `json:"line"`
	CLIVersion        string     `json:"cli_version,omitempty"`
	Source            SourceKind `json:"source,omitempty"`
	Agent             string     `json:"agent,omitempty"`
	Provider          string     `json:"provider,omitempty"`
	ProviderSessionID string     `json:"provider_session_id,omitempty"`
	CtxSessionID      string     `json:"ctx_session_id,omitempty"`
	EventID           string     `json:"event_id,omitempty"`
}

// ToolLayer identifies where a tool observation was made.
type ToolLayer string

const (
	LayerModel     ToolLayer = "model"
	LayerRuntime   ToolLayer = "runtime"
	LayerEffective ToolLayer = "effective"
)

// ToolStatus describes whether a completed tool call succeeded.
type ToolStatus string

const (
	StatusSuccess ToolStatus = "success"
	StatusFailure ToolStatus = "failure"
	StatusUnknown ToolStatus = "unknown"
)

// Turn is the bounded normalization unit for a conversation turn.
type Turn struct {
	SessionID       string            `json:"session_id"`
	ID              string            `json:"id"`
	Ordinal         int               `json:"ordinal"`
	StartedAt       time.Time         `json:"started_at,omitempty"`
	EndedAt         time.Time         `json:"ended_at,omitempty"`
	Aborted         bool              `json:"aborted,omitempty"`
	Source          SourceRef         `json:"source"`
	UserPrompts     int               `json:"user_prompts,omitempty"`
	UserPromptTimes []time.Time       `json:"-"`
	ModelTools      []ToolObservation `json:"model_tools,omitempty"`
	RuntimeTools    []ToolObservation `json:"runtime_tools,omitempty"`
	SkillEvidence   []SkillEvidence   `json:"skill_evidence,omitempty"`
}

// ToolObservation is a model or runtime observation of one tool invocation.
type ToolObservation struct {
	SessionID     string     `json:"session_id"`
	TurnID        string     `json:"turn_id"`
	RawName       string     `json:"raw_name"`
	CanonicalName string     `json:"canonical_name"`
	CallID        string     `json:"call_id,omitempty"`
	ItemID        string     `json:"item_id,omitempty"`
	Arguments     string     `json:"arguments,omitempty"`
	Timestamp     time.Time  `json:"timestamp"`
	Layer         ToolLayer  `json:"layer"`
	Status        ToolStatus `json:"status"`
	Source        SourceRef  `json:"source"`
}

// SkillEvidenceMethod describes how a skill use was detected.
type SkillEvidenceMethod string

const (
	MethodExplicitInjected          SkillEvidenceMethod = "explicit-injected"
	MethodSelectedSkillInstructions SkillEvidenceMethod = "selected-skill-instructions"
	MethodSkillInjection            SkillEvidenceMethod = "skill-injected"
	MethodRuntimeSkillItem          SkillEvidenceMethod = "runtime-skill-item"
	MethodStructuredTool            SkillEvidenceMethod = "structured-tool"
	MethodExplicitRequest           SkillEvidenceMethod = "explicit-request"
	MethodImplicitAccess            SkillEvidenceMethod = "implicit-access"
)

type SkillMode string

const (
	ModeExplicit SkillMode = "explicit"
	ModeImplicit SkillMode = "implicit"
	ModeUnknown  SkillMode = "unknown"
)

type SkillState string

const (
	StateConfirmed   SkillState = "confirmed"
	StateInferred    SkillState = "inferred"
	StateUnconfirmed SkillState = "unconfirmed"
)

// SkillEvidence is one independently collected indication of skill use.
type SkillEvidence struct {
	SessionID string              `json:"session_id"`
	TurnID    string              `json:"turn_id"`
	SkillName string              `json:"skill_name"`
	Mode      SkillMode           `json:"mode"`
	Method    SkillEvidenceMethod `json:"method"`
	State     SkillState          `json:"state"`
	Timestamp time.Time           `json:"timestamp"`
	Source    SourceRef           `json:"source"`
}

// SkillUse is the deduplicated representation of evidence for one turn.
type SkillUse struct {
	SessionID string                `json:"session_id"`
	TurnID    string                `json:"turn_id"`
	SkillName string                `json:"skill_name"`
	Mode      SkillMode             `json:"mode"`
	Modes     []SkillMode           `json:"modes,omitempty"`
	Methods   []SkillEvidenceMethod `json:"methods"`
	State     SkillState            `json:"state"`
	Timestamp time.Time             `json:"timestamp"`
	Source    SourceRef             `json:"source"`
}

// Warning records a recoverable input problem.
type Warning struct {
	Reason string `json:"reason"`
	Type   string `json:"type,omitempty"`
	Path   string `json:"path,omitempty"`
	Line   int    `json:"line,omitempty"`
	Count  int    `json:"count"`
}

// Result is the typed aggregate input shared by renderers.
type Result struct {
	Sessions    int               `json:"sessions"`
	UserPrompts int               `json:"user_prompts"`
	ToolCalls   []ToolObservation `json:"tool_calls"`
	SkillUses   []SkillUse        `json:"skill_uses"`
	Warnings    []Warning         `json:"warnings,omitempty"`
}

func NewSourceRef(path string, line int, cliVersion string) SourceRef {
	return SourceRef{Path: path, Line: line, CLIVersion: cliVersion}
}

func NewCodexSourceRef(path string, line int, cliVersion string) SourceRef {
	source := NewSourceRef(path, line, cliVersion)
	source.Source = SourceCodex
	source.Agent = "codex"
	source.Provider = "codex"
	return source
}

func NewCtxSourceRef(path, provider, providerSessionID, ctxSessionID, eventID string) SourceRef {
	agent := CanonicalAgentID(provider)
	return SourceRef{
		Path:              path,
		Source:            SourceCtx,
		Agent:             agent,
		Provider:          strings.TrimSpace(provider),
		ProviderSessionID: strings.TrimSpace(providerSessionID),
		CtxSessionID:      strings.TrimSpace(ctxSessionID),
		EventID:           strings.TrimSpace(eventID),
	}
}

func NewTurn(sessionID, id string, ordinal int, source SourceRef) Turn {
	return Turn{SessionID: sessionID, ID: id, Ordinal: ordinal, Source: source}
}

func NewToolObservation(sessionID, turnID, rawName, canonicalName string, layer ToolLayer, status ToolStatus, timestamp time.Time, source SourceRef) ToolObservation {
	return ToolObservation{SessionID: sessionID, TurnID: turnID, RawName: rawName, CanonicalName: canonicalName, Layer: layer, Status: status, Timestamp: timestamp, Source: source}
}

func NewSkillEvidence(sessionID, turnID, skill string, mode SkillMode, method SkillEvidenceMethod, state SkillState, timestamp time.Time, source SourceRef) SkillEvidence {
	return SkillEvidence{SessionID: sessionID, TurnID: turnID, SkillName: skill, Mode: mode, Method: method, State: state, Timestamp: timestamp, Source: source}
}

func NewSkillUse(sessionID, turnID, skill string, mode SkillMode, state SkillState, timestamp time.Time, source SourceRef) SkillUse {
	use := SkillUse{SessionID: sessionID, TurnID: turnID, SkillName: skill, Mode: mode, State: state, Timestamp: timestamp, Source: source}
	if mode != "" {
		use.Modes = []SkillMode{mode}
	}
	return use
}

// HasMode reports whether a deduplicated use has evidence for mode. Older
// callers may construct SkillUse values without Modes, so Mode remains a
// compatible fallback.
func (u SkillUse) HasMode(mode SkillMode) bool {
	for _, observed := range u.Modes {
		if observed == mode {
			return true
		}
	}
	return len(u.Modes) == 0 && u.Mode == mode
}
