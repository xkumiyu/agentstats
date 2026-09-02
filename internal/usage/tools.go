package usage

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// NormalizeModelCall converts a response_item function/custom tool call into
// the model layer. Unknown fields are intentionally ignored so newer Codex
// records remain readable.
func NormalizeModelCall(sessionID, turnID string, raw map[string]any, timestamp time.Time, source SourceRef) (ToolObservation, bool) {
	typ := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(stringValue(raw, "type"), "-", "_"), " ", "_"))
	if typ != "function_call" && typ != "custom_tool_call" && typ != "functioncall" && typ != "customtoolcall" {
		return ToolObservation{}, false
	}
	name := strings.TrimSpace(firstString(raw, "name", "tool_name", "tool"))
	if name == "" {
		return ToolObservation{}, false
	}
	callID := firstString(raw, "call_id", "callId", "id", "item_id", "itemId")
	arguments := firstRawText(raw, "arguments", "input")
	obs := NewToolObservation(sessionID, turnID, name, CanonicalModelToolName(name), LayerModel, modelStatus(raw), timestamp, source)
	obs.CallID = callID
	obs.Arguments = arguments
	return obs, true
}

// NormalizeRuntimeItem converts an event_msg ItemCompleted item into a
// runtime observation. The item type spelling has changed between Codex
// versions, so matching is case- and separator-insensitive.
func NormalizeRuntimeItem(sessionID, turnID string, raw map[string]any, timestamp time.Time, source SourceRef) (ToolObservation, bool) {
	typ := compactType(stringValue(raw, "type"))
	canonical, ok := canonicalRuntimeName(typ, raw)
	if !ok {
		return ToolObservation{}, false
	}
	rawName := firstString(raw, "name", "tool_name", "tool", "type")
	if rawName == "" {
		rawName = stringValue(raw, "type")
	}
	callID := firstString(raw, "call_id", "callId", "id", "item_id", "itemId")
	arguments := runtimeArguments(raw)
	obs := NewToolObservation(sessionID, turnID, rawName, canonical, LayerRuntime, runtimeStatus(raw), timestamp, source)
	obs.CallID = callID
	obs.ItemID = firstString(raw, "item_id", "itemId", "id")
	obs.Arguments = arguments
	return obs, true
}

// CanonicalModelToolName keeps model names stable while normalizing only the
// code-mode wrapper that has a well-known spelling.
func CanonicalModelToolName(name string) string {
	name = strings.TrimSpace(name)
	if strings.EqualFold(name, "exec") {
		return "exec"
	}
	return name
}

// EffectiveTools chooses runtime observations over their model wrappers for
// normal usage statistics. It never mutates the observations stored on Turn.
func EffectiveTools(turn Turn) []ToolObservation {
	if len(turn.RuntimeTools) == 0 {
		return effectiveCopies(turn.ModelTools)
	}
	runtime := dedupeRuntime(turn.RuntimeTools)
	ids := make(map[string]struct{}, len(runtime)*2)
	canonical := make(map[string]struct{}, len(runtime))
	for _, obs := range runtime {
		if obs.CallID != "" {
			ids[obs.CallID] = struct{}{}
		}
		if obs.ItemID != "" {
			ids[obs.ItemID] = struct{}{}
		}
		if obs.CanonicalName != "" {
			canonical[obs.CanonicalName] = struct{}{}
		}
	}
	result := make([]ToolObservation, 0, len(runtime))
	for _, obs := range runtime {
		obs.Layer = LayerEffective
		result = append(result, obs)
	}
	for _, obs := range turn.ModelTools {
		if strings.EqualFold(obs.CanonicalName, "exec") || strings.EqualFold(obs.RawName, "exec") {
			continue
		}
		if obs.CallID != "" {
			if _, found := ids[obs.CallID]; found {
				continue
			}
		}
		if _, found := canonical[obs.CanonicalName]; found && obs.CanonicalName != "" {
			continue
		}
		obs.Layer = LayerEffective
		result = append(result, obs)
	}
	return result
}

// ToolsForLayer returns a copy of the requested view.
func ToolsForLayer(turn Turn, layer ToolLayer) []ToolObservation {
	switch layer {
	case LayerModel:
		return copiesWithLayer(turn.ModelTools, LayerModel)
	case LayerRuntime:
		return copiesWithLayer(turn.RuntimeTools, LayerRuntime)
	case LayerEffective, "":
		return EffectiveTools(turn)
	default:
		return nil
	}
}

func copiesWithLayer(in []ToolObservation, layer ToolLayer) []ToolObservation {
	out := make([]ToolObservation, 0, len(in))
	for _, obs := range in {
		obs.Layer = layer
		out = append(out, obs)
	}
	return out
}

func effectiveCopies(in []ToolObservation) []ToolObservation {
	out := make([]ToolObservation, 0, len(in))
	for _, obs := range in {
		obs.Layer = LayerEffective
		out = append(out, obs)
	}
	return out
}

func dedupeRuntime(in []ToolObservation) []ToolObservation {
	seen := make(map[string]struct{}, len(in))
	out := make([]ToolObservation, 0, len(in))
	for _, obs := range in {
		key := ""
		if obs.ItemID != "" {
			key = "item:" + obs.ItemID
		} else if obs.CallID != "" {
			key = "call:" + obs.CallID
		}
		if key != "" {
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
		}
		out = append(out, obs)
	}
	return out
}

func canonicalRuntimeName(typ string, raw map[string]any) (string, bool) {
	switch {
	case strings.Contains(typ, "commandexecution"), typ == "command", strings.Contains(typ, "shellcommand"):
		return "shell", true
	case strings.Contains(typ, "mcptoolcall"), strings.Contains(typ, "mcp_tool"):
		server := firstString(raw, "server", "server_name", "serverName", "mcp_server", "mcpServer")
		tool := firstString(raw, "tool", "tool_name", "toolName", "name")
		if server == "" {
			server = "unknown"
		}
		if tool == "" {
			tool = "unknown"
		}
		return "mcp:" + server + "/" + tool, true
	case strings.Contains(typ, "filechange"), strings.Contains(typ, "file_change"):
		return "file_change", true
	case strings.Contains(typ, "websearch"), strings.Contains(typ, "web_search"):
		return "web_search", true
	case strings.Contains(typ, "imageview"), strings.Contains(typ, "image_view"):
		return "image_view", true
	case strings.Contains(typ, "imagegeneration"), strings.Contains(typ, "image_generation"):
		return "image_generation", true
	case strings.Contains(typ, "collabagenttoolcall"), strings.Contains(typ, "collaboration"), strings.Contains(typ, "collab"):
		tool := firstString(raw, "tool", "tool_name", "name")
		if tool == "" {
			return "collaboration", true
		}
		return "collaboration:" + tool, true
	default:
		return "", false
	}
}

func compactType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "")
	value = strings.ReplaceAll(value, "-", "")
	value = strings.ReplaceAll(value, " ", "")
	return value
}

func modelStatus(raw map[string]any) ToolStatus {
	return statusFromMap(raw, StatusUnknown)
}

func runtimeStatus(raw map[string]any) ToolStatus {
	return statusFromMap(raw, StatusSuccess)
}

func statusFromMap(raw map[string]any, fallback ToolStatus) ToolStatus {
	if value, ok := raw["success"].(bool); ok {
		if value {
			return StatusSuccess
		}
		return StatusFailure
	}
	if value, ok := raw["is_error"].(bool); ok && value {
		return StatusFailure
	}
	if value, ok := numberValue(raw["exit_code"]); ok && value != 0 {
		return StatusFailure
	}
	if raw["error"] != nil {
		return StatusFailure
	}
	for _, key := range []string{"status", "state", "result"} {
		value := strings.ToLower(strings.TrimSpace(stringValue(raw, key)))
		switch value {
		case "success", "succeeded", "ok", "completed", "complete", "done":
			return StatusSuccess
		case "failure", "failed", "error", "errored", "cancelled", "canceled", "aborted":
			return StatusFailure
		}
	}
	return fallback
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func runtimeArguments(raw map[string]any) string {
	for _, key := range []string{"command", "cmd", "script", "path"} {
		if value := rawText(raw, key); value != "" {
			return value
		}
	}
	for _, key := range []string{"input", "arguments"} {
		if value := rawText(raw, key); value != "" {
			return value
		}
	}
	return ""
}

func firstString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(raw, key); value != "" {
			return value
		}
	}
	return ""
}

func stringValue(raw map[string]any, key string) string {
	value, ok := raw[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case fmt.Stringer:
		return typed.String()
	case float64:
		return fmt.Sprintf("%g", typed)
	default:
		return ""
	}
}

func rawText(raw map[string]any, key string) string {
	value, ok := raw[key]
	if !ok || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func firstRawText(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := rawText(raw, key); value != "" {
			return value
		}
	}
	return ""
}
