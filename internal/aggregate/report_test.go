package aggregate

import (
	"testing"
	"time"

	"github.com/xkumiyu/agentstats/internal/usage"
)

func TestBuildOverviewAndLayerAggregation(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	turn := usage.Turn{
		SessionID:    "s",
		UserPrompts:  1,
		ModelTools:   []usage.ToolObservation{{RawName: "exec", CanonicalName: "exec", Layer: usage.LayerModel, Timestamp: now}, {CanonicalName: "web_search", Layer: usage.LayerModel, Timestamp: now}},
		RuntimeTools: []usage.ToolObservation{{CanonicalName: "shell", Layer: usage.LayerRuntime, Status: usage.StatusFailure, Timestamp: now}},
		SkillEvidence: []usage.SkillEvidence{
			usage.NewSkillEvidence("s", "t", "report", usage.ModeExplicit, usage.MethodExplicitInjected, usage.StateConfirmed, now, usage.SourceRef{}),
			usage.NewSkillEvidence("s", "t", "report", usage.ModeExplicit, usage.MethodExplicitRequest, usage.StateUnconfirmed, now, usage.SourceRef{}),
		},
	}
	input := Input{Turns: []usage.Turn{turn}, SessionCount: 1}
	report := BuildOverview(input)
	if report.Overview.Sessions != 1 || report.Overview.UserPrompts != 1 || report.Overview.ToolCalls != 2 || report.Overview.SkillUses != 1 {
		t.Fatalf("overview = %#v", report.Overview)
	}
	if len(report.Tools) != 2 || report.Tools[0].Name != "shell" || report.Tools[0].Failures != 1 {
		t.Fatalf("effective rows = %#v", report.Tools)
	}
	model := Tools(input, usage.LayerModel)
	if len(model) != 2 || model[0].Name != "exec" {
		t.Fatalf("model rows = %#v", model)
	}
	strict := Skills(input, true)
	if len(strict) != 1 || strict[0].Total != 1 || strict[0].Confirmed != 1 {
		t.Fatalf("strict rows = %#v", strict)
	}
}

func TestAggregateSortsByCountThenName(t *testing.T) {
	turns := []usage.Turn{
		{SessionID: "s", RuntimeTools: []usage.ToolObservation{{CanonicalName: "z", Status: usage.StatusSuccess}, {CanonicalName: "a", Status: usage.StatusSuccess}}},
		{SessionID: "s", RuntimeTools: []usage.ToolObservation{{CanonicalName: "a", Status: usage.StatusSuccess}}},
	}
	rows := Tools(Input{Turns: turns}, usage.LayerRuntime)
	if len(rows) != 2 || rows[0].Name != "a" || rows[1].Name != "z" {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestSkillsPreserveOverlappingModesAndUnknownActivation(t *testing.T) {
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	turns := []usage.Turn{
		{SessionID: "s", ID: "explicit-and-implicit", SkillEvidence: []usage.SkillEvidence{
			usage.NewSkillEvidence("s", "explicit-and-implicit", "report", usage.ModeExplicit, usage.MethodExplicitRequest, usage.StateUnconfirmed, stamp, usage.SourceRef{}),
			usage.NewSkillEvidence("s", "explicit-and-implicit", "report", usage.ModeImplicit, usage.MethodImplicitAccess, usage.StateInferred, stamp, usage.SourceRef{}),
		}},
		{SessionID: "s", ID: "unknown", SkillEvidence: []usage.SkillEvidence{
			usage.NewSkillEvidence("s", "unknown", "report", usage.ModeUnknown, usage.MethodSelectedSkillInstructions, usage.StateConfirmed, stamp.Add(time.Second), usage.SourceRef{}),
		}},
	}
	rows := Skills(Input{Turns: turns}, false)
	if len(rows) != 1 {
		t.Fatalf("rows = %#v", rows)
	}
	row := rows[0]
	if row.Total != 2 || row.Explicit != 1 || row.Implicit != 1 || row.Unknown != 1 || row.Confirmed != 1 || row.Inferred != 1 || row.Unconfirmed != 0 {
		t.Fatalf("skill row = %#v", row)
	}
}
