package usage

import (
	"testing"
	"time"
)

func TestNormalizeModelCall(t *testing.T) {
	obs, ok := NormalizeModelCall("s", "t", map[string]any{
		"type":    "custom_tool_call",
		"name":    "exec",
		"call_id": "call-1",
		"input":   "{\"cmd\":\"pwd\"}",
	}, time.Unix(10, 0), SourceRef{Path: "x", Line: 2})
	if !ok {
		t.Fatal("expected model call")
	}
	if obs.Layer != LayerModel || obs.CanonicalName != "exec" || obs.CallID != "call-1" || obs.Arguments == "" {
		t.Fatalf("unexpected observation: %#v", obs)
	}
}

func TestNormalizeRuntimeItems(t *testing.T) {
	cases := []struct {
		typ, want string
	}{
		{"CommandExecution", "shell"},
		{"McpToolCall", "mcp:server/tool"},
		{"FileChange", "file_change"},
		{"WebSearch", "web_search"},
		{"ImageView", "image_view"},
		{"ImageGeneration", "image_generation"},
		{"CollabAgentToolCall", "collaboration:delegate"},
	}
	for _, tc := range cases {
		raw := map[string]any{"type": tc.typ, "name": "delegate"}
		if tc.typ == "McpToolCall" {
			raw["server"] = "server"
			raw["tool"] = "tool"
		}
		obs, ok := NormalizeRuntimeItem("s", "t", raw, time.Unix(10, 0), SourceRef{})
		if !ok || obs.CanonicalName != tc.want || obs.Layer != LayerRuntime {
			t.Errorf("%s => %#v, ok=%v", tc.typ, obs, ok)
		}
	}
	failed, ok := NormalizeRuntimeItem("s", "t", map[string]any{"type": "CommandExecution", "status": "completed", "exit_code": float64(1)}, time.Unix(10, 0), SourceRef{})
	if !ok || failed.Status != StatusFailure {
		t.Fatalf("nonzero exit code status = %#v, ok=%v", failed, ok)
	}
}

func TestEffectiveToolsSuppressExecAndDeduplicatesRuntimeID(t *testing.T) {
	turn := Turn{
		ModelTools: []ToolObservation{
			{RawName: "exec", CanonicalName: "exec", CallID: "wrapper"},
			{RawName: "other", CanonicalName: "other", CallID: "runtime-1"},
		},
		RuntimeTools: []ToolObservation{
			{CanonicalName: "shell", ItemID: "runtime-1"},
			{CanonicalName: "shell", ItemID: "runtime-1"},
			{CanonicalName: "file_change", ItemID: "runtime-2"},
		},
	}
	got := EffectiveTools(turn)
	if len(got) != 2 {
		t.Fatalf("effective tools = %#v", got)
	}
	if got[0].Layer != LayerEffective || got[0].CanonicalName != "shell" || got[1].CanonicalName != "file_change" {
		t.Fatalf("unexpected effective tools: %#v", got)
	}
}

func TestEffectiveToolsFallsBackToModel(t *testing.T) {
	turn := Turn{ModelTools: []ToolObservation{{CanonicalName: "web_search", Layer: LayerModel}}}
	got := EffectiveTools(turn)
	if len(got) != 1 || got[0].Layer != LayerEffective {
		t.Fatalf("fallback = %#v", got)
	}
}

func TestEffectiveToolsSuppressesMatchingNonExecCanonical(t *testing.T) {
	turn := Turn{
		ModelTools:   []ToolObservation{{RawName: "web_search", CanonicalName: "web_search"}},
		RuntimeTools: []ToolObservation{{RawName: "WebSearch", CanonicalName: "web_search", ItemID: "item-1"}},
	}
	got := EffectiveTools(turn)
	if len(got) != 1 || got[0].CanonicalName != "web_search" {
		t.Fatalf("effective tools = %#v", got)
	}
}

func TestToolsForLayerPreservesObservedLayer(t *testing.T) {
	turn := Turn{ModelTools: []ToolObservation{{CanonicalName: "exec", Layer: LayerModel}}, RuntimeTools: []ToolObservation{{CanonicalName: "shell", Layer: LayerRuntime}}}
	if got := ToolsForLayer(turn, LayerModel); len(got) != 1 || got[0].Layer != LayerModel {
		t.Fatalf("model layer = %#v", got)
	}
	if got := ToolsForLayer(turn, LayerRuntime); len(got) != 1 || got[0].Layer != LayerRuntime {
		t.Fatalf("runtime layer = %#v", got)
	}
}
