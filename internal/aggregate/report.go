package aggregate

import (
	"sort"
	"time"

	"github.com/xkumiyu/agentstats/internal/usage"
)

// Input is the normalized stream consumed by all report views.
type Input struct {
	Turns        []usage.Turn
	SessionCount int
	Warnings     []usage.Warning
}

type Overview struct {
	Sessions    int
	UserPrompts int
	ToolCalls   int
	SkillUses   int
}

type ToolRow struct {
	Name     string
	Calls    int
	Failures int
	LastUsed time.Time
}

type SkillRow struct {
	Name        string
	Explicit    int
	Implicit    int
	Unknown     int
	Confirmed   int
	Inferred    int
	Unconfirmed int
	Total       int
	LastUsed    time.Time
}

type Report struct {
	Overview Overview
	Tools    []ToolRow
	Skills   []SkillRow
	Warnings []usage.Warning
}

// BuildOverview computes all aggregate views from normalized turns. The
// effective view is used for overview Tool Calls, while model/runtime rows are
// retained for the tools command.
func BuildOverview(input Input) Report {
	result := Report{Warnings: append([]usage.Warning(nil), input.Warnings...)}
	sessions := make(map[string]struct{})
	allSkills := make([]usage.SkillEvidence, 0)
	for _, turn := range input.Turns {
		if turn.SessionID != "" {
			sessions[turn.SessionID] = struct{}{}
		}
		result.Overview.UserPrompts += turn.UserPrompts
		effective := usage.EffectiveTools(turn)
		result.Overview.ToolCalls += len(effective)
		allSkills = append(allSkills, turn.SkillEvidence...)
	}
	result.Overview.Sessions = input.SessionCount
	if result.Overview.Sessions == 0 {
		result.Overview.Sessions = len(sessions)
	}
	uses := usage.MergeSkillEvidence(allSkills)
	result.Overview.SkillUses = len(uses)
	result.Tools = aggregateTools(input.Turns, usage.LayerEffective)
	result.Skills = aggregateSkills(uses, false)
	return result
}

func Tools(input Input, layer usage.ToolLayer) []ToolRow {
	return aggregateTools(input.Turns, layer)
}

func Skills(input Input, strict bool) []SkillRow {
	evidence := make([]usage.SkillEvidence, 0)
	for _, turn := range input.Turns {
		evidence = append(evidence, turn.SkillEvidence...)
	}
	return aggregateSkills(usage.MergeSkillEvidence(evidence), strict)
}

func aggregateTools(turns []usage.Turn, layer usage.ToolLayer) []ToolRow {
	rows := make(map[string]*ToolRow)
	for _, turn := range turns {
		observations := usage.ToolsForLayer(turn, layer)
		for _, obs := range observations {
			name := obs.CanonicalName
			if name == "" {
				name = obs.RawName
			}
			row := rows[name]
			if row == nil {
				row = &ToolRow{Name: name}
				rows[name] = row
			}
			row.Calls++
			if obs.Status == usage.StatusFailure {
				row.Failures++
			}
			if row.LastUsed.IsZero() || obs.Timestamp.After(row.LastUsed) {
				row.LastUsed = obs.Timestamp
			}
		}
	}
	result := make([]ToolRow, 0, len(rows))
	for _, row := range rows {
		result = append(result, *row)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Calls == result[j].Calls {
			return result[i].Name < result[j].Name
		}
		return result[i].Calls > result[j].Calls
	})
	return result
}

func aggregateSkills(uses []usage.SkillUse, strict bool) []SkillRow {
	rows := make(map[string]*SkillRow)
	for _, use := range uses {
		if strict && use.State != usage.StateConfirmed {
			continue
		}
		row := rows[use.SkillName]
		if row == nil {
			row = &SkillRow{Name: use.SkillName}
			rows[use.SkillName] = row
		}
		row.Total++
		if use.HasMode(usage.ModeExplicit) {
			row.Explicit++
		}
		if use.HasMode(usage.ModeImplicit) {
			row.Implicit++
		}
		if use.HasMode(usage.ModeUnknown) {
			row.Unknown++
		}
		switch use.State {
		case usage.StateConfirmed:
			row.Confirmed++
		case usage.StateInferred:
			row.Inferred++
		case usage.StateUnconfirmed:
			row.Unconfirmed++
		}
		if row.LastUsed.IsZero() || use.Timestamp.After(row.LastUsed) {
			row.LastUsed = use.Timestamp
		}
	}
	result := make([]SkillRow, 0, len(rows))
	for _, row := range rows {
		result = append(result, *row)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Total == result[j].Total {
			return result[i].Name < result[j].Name
		}
		return result[i].Total > result[j].Total
	})
	return result
}
