package hooks

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/abbayosua/skynet/internal/shell"
	"github.com/tidwall/gjson"
)

// SupportedOutputVersion is the highest envelope version this build
// understands. Hooks may omit `version` entirely (treated as 1) or pin
// an older version. Unknown higher versions are still parsed but logged.
const SupportedOutputVersion = 1

// Payload is the JSON structure piped to hook commands via stdin.
// ToolInput is emitted as a parsed JSON object for compatibility with
// Claude Code hooks (which expect tool_input to be an object, not a
// string).
type Payload struct {
	Event     string          `json:"event"`
	SessionID string          `json:"session_id"`
	CWD       string          `json:"cwd"`
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
}

// PostPayload is the JSON structure for PostToolUse hooks. It extends
// Payload with tool output so hooks like omni can distill it. It includes
// multiple alias fields for host compatibility (Claude Code uses
// hookEventName, some hosts use hook_event_name, skynet uses event).
type PostPayload struct {
	Event         string          `json:"event"`
	HookEventName string          `json:"hookEventName"`
	HookEventName2 string         `json:"hook_event_name"`
	SessionID     string          `json:"session_id"`
	SessionID2    string          `json:"sessionId"`
	CWD           string          `json:"cwd"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	ToolResponse  json.RawMessage `json:"tool_response"`
	ToolOutput    string          `json:"tool_output"`
	ToolIsError   bool            `json:"tool_is_error"`
}

// BuildPayload constructs the JSON stdin payload for a hook command.
func BuildPayload(eventName, sessionID, cwd, toolName, toolInputJSON string) []byte {
	toolInput := json.RawMessage(toolInputJSON)
	if !json.Valid(toolInput) {
		toolInput = json.RawMessage("{}")
	}
	p := Payload{
		Event:     eventName,
		SessionID: sessionID,
		CWD:       cwd,
		ToolName:  toolName,
		ToolInput: toolInput,
	}
	data, err := json.Marshal(p)
	if err != nil {
		return []byte("{}")
	}
	return data
}

// BuildPostPayload constructs the JSON stdin payload for PostToolUse.
// toolOutput is the raw text output from the tool, isError indicates failure.
func BuildPostPayload(eventName, sessionID, cwd, toolName, toolInputJSON, toolOutput string, isError bool) []byte {
	toolInput := json.RawMessage(toolInputJSON)
	if !json.Valid(toolInput) {
		toolInput = json.RawMessage("{}")
	}
	// Build tool_response in shape omni expects for ClaudeCode: stdout/content
	respObj := map[string]any{
		"content": toolOutput,
		"stdout":  toolOutput,
	}
	// For file tools, also provide file.content shape for Read distiller
	if toolName == "view" || toolName == "read" {
		respObj["file"] = map[string]any{"content": toolOutput}
	}
	respBytes, _ := json.Marshal(respObj)
	p := PostPayload{
		Event:          eventName,
		HookEventName:  eventName,
		HookEventName2: eventName,
		SessionID:      sessionID,
		SessionID2:     sessionID,
		CWD:            cwd,
		ToolName:       toolName,
		ToolInput:      toolInput,
		ToolResponse:   json.RawMessage(respBytes),
		ToolOutput:     toolOutput,
		ToolIsError:    isError,
	}
	data, err := json.Marshal(p)
	if err != nil {
		return []byte("{}")
	}
	return data
}

// BuildEnv constructs the environment variable slice for a hook command.
// It includes all current process env vars plus hook-specific ones.
func BuildEnv(eventName, toolName, sessionID, cwd, projectDir, toolInputJSON string) []string {
	env := os.Environ()
	env = append(env, shell.SkyNetEnvMarkers()...)
	env = append(env,
		// CRUSH_* vars for backward compatibility
		fmt.Sprintf("CRUSH_EVENT=%s", eventName),
		fmt.Sprintf("CRUSH_TOOL_NAME=%s", toolName),
		fmt.Sprintf("CRUSH_SESSION_ID=%s", sessionID),
		fmt.Sprintf("CRUSH_CWD=%s", cwd),
		fmt.Sprintf("CRUSH_PROJECT_DIR=%s", projectDir),
		// SKYNET_* vars (preferred)
		fmt.Sprintf("SKYNET_EVENT=%s", eventName),
		fmt.Sprintf("SKYNET_TOOL_NAME=%s", toolName),
		fmt.Sprintf("SKYNET_SESSION_ID=%s", sessionID),
		fmt.Sprintf("SKYNET_CWD=%s", cwd),
		fmt.Sprintf("SKYNET_PROJECT_DIR=%s", projectDir),
	)

	// Extract tool-specific env vars from the JSON input.
	if toolInputJSON != "" {
		if cmd := gjson.Get(toolInputJSON, "command"); cmd.Exists() {
			env = append(env, fmt.Sprintf("CRUSH_TOOL_INPUT_COMMAND=%s", cmd.String()))
			env = append(env, fmt.Sprintf("SKYNET_TOOL_INPUT_COMMAND=%s", cmd.String()))
		}
		if fp := gjson.Get(toolInputJSON, "file_path"); fp.Exists() {
			env = append(env, fmt.Sprintf("CRUSH_TOOL_INPUT_FILE_PATH=%s", fp.String()))
			env = append(env, fmt.Sprintf("SKYNET_TOOL_INPUT_FILE_PATH=%s", fp.String()))
		}
	}

	return env
}

// parseStdout parses the JSON output from a hook command's stdout.
// Supports both Crush format and Claude Code format (hookSpecificOutput).
func parseStdout(stdout string) HookResult {
	stdout = strings.TrimSpace(stdout)
	if stdout == "" {
		return HookResult{Decision: DecisionNone}
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
		return HookResult{Decision: DecisionNone}
	}

	// Claude Code compat: if hookSpecificOutput is present, parse that.
	// This handles both PreToolUse (permissionDecision) and PostToolUse (updatedToolOutput) including omni's shape.
	if hso, ok := raw["hookSpecificOutput"]; ok {
		return parseClaudeCodeOutput(hso)
	}
	// Also handle omni's direct shape without outer wrapper? omni's post is inside hookSpecificOutput, already handled.
	// Some hosts use "hookSpecificOutput" with different casing, but we already handled the primary.

	var parsed struct {
		Version       int             `json:"version"`
		Decision      string          `json:"decision"`
		Halt          bool            `json:"halt"`
		Reason        string          `json:"reason"`
		Context       json.RawMessage `json:"context"`
		UpdatedInput  json.RawMessage `json:"updated_input"`
		UpdatedOutput json.RawMessage `json:"updated_output"`
		// Also accept camelCase and alternative names for PostToolUse
		UpdatedOutput2 json.RawMessage `json:"updatedOutput"`
		UpdatedOutput3 json.RawMessage `json:"updated_tool_output"`
		AdditionalContext json.RawMessage `json:"additionalContext"`
	}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		return HookResult{Decision: DecisionNone}
	}

	if parsed.Version > SupportedOutputVersion {
		slog.Debug("Hook output declared a newer envelope version than this build supports",
			"version", parsed.Version,
			"supported", SupportedOutputVersion,
		)
	}

	// Context may be in "context" or "additionalContext" (omni's Post shape when not wrapped)
	ctxRaw := parsed.Context
	if len(ctxRaw) == 0 || string(ctxRaw) == "null" {
		ctxRaw = parsed.AdditionalContext
	}
	result := HookResult{
		Halt:    parsed.Halt,
		Reason:  parsed.Reason,
		Context: parseContext(ctxRaw),
	}
	result.Decision = parseDecision(parsed.Decision)
	result.UpdatedInput = rawToString(parsed.UpdatedInput)
	// For PostToolUse, updated_output may be in multiple fields; first writer wins for now, aggregate handles ordering
	if len(parsed.UpdatedOutput) > 0 && string(parsed.UpdatedOutput) != "null" {
		result.UpdatedOutput = extractUpdatedOutput(parsed.UpdatedOutput)
	} else if len(parsed.UpdatedOutput2) > 0 && string(parsed.UpdatedOutput2) != "null" {
		result.UpdatedOutput = extractUpdatedOutput(parsed.UpdatedOutput2)
	} else if len(parsed.UpdatedOutput3) > 0 && string(parsed.UpdatedOutput3) != "null" {
		result.UpdatedOutput = extractUpdatedOutput(parsed.UpdatedOutput3)
	}
	return result
}

// parseContext accepts either a single string or an array of strings and
// returns a newline-joined value with empty entries dropped.
func parseContext(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	// String form.
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
		return ""
	}
	// Array form.
	if raw[0] == '[' {
		var items []string
		if err := json.Unmarshal(raw, &items); err != nil {
			return ""
		}
		out := items[:0]
		for _, s := range items {
			if s != "" {
				out = append(out, s)
			}
		}
		return strings.Join(out, "\n")
	}
	return ""
}

// parseClaudeCodeOutput handles the Claude Code hook output format:
// {"hookSpecificOutput": {"permissionDecision": "allow", ...}} for PreToolUse
// and {"hookSpecificOutput": {"hookEventName": "PostToolUse", "updatedToolOutput": {...}, "additionalContext": "..."}} for PostToolUse (omni)
func parseClaudeCodeOutput(data json.RawMessage) HookResult {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return HookResult{Decision: DecisionNone}
	}
	// Check for PostToolUse shape first (omni): updatedToolOutput + additionalContext
	if upd, ok := raw["updatedToolOutput"]; ok {
		// Also handle alternative casings: updated_tool_output
		result := HookResult{}
		result.UpdatedOutput = extractUpdatedOutput(upd)
		if ctx, ok := raw["additionalContext"]; ok {
			result.Context = parseContext(ctx)
		} else if ctx, ok := raw["additional_context"]; ok {
			result.Context = parseContext(ctx)
		}
		// PostToolUse may also have decision/halt? Usually not, but handle
		if dec, ok := raw["permissionDecision"]; ok {
			var s string
			_ = json.Unmarshal(dec, &s)
			result.Decision = parseDecision(s)
		}
		if dec, ok := raw["decision"]; ok {
			var s string
			_ = json.Unmarshal(dec, &s)
			if result.Decision == DecisionNone {
				result.Decision = parseDecision(s)
			}
		}
		return result
	}
	if upd, ok := raw["updated_tool_output"]; ok {
		result := HookResult{}
		result.UpdatedOutput = extractUpdatedOutput(upd)
		if ctx, ok := raw["additionalContext"]; ok {
			result.Context = parseContext(ctx)
		}
		return result
	}
	// Also handle skynet native Post shape inside hookSpecificOutput: updated_output
	if upd, ok := raw["updated_output"]; ok {
		result := HookResult{}
		result.UpdatedOutput = extractUpdatedOutput(upd)
		if ctx, ok := raw["additionalContext"]; ok {
			result.Context = parseContext(ctx)
		} else if ctx, ok := raw["context"]; ok {
			result.Context = parseContext(ctx)
		}
		return result
	}
	if upd, ok := raw["updatedOutput"]; ok {
		result := HookResult{}
		result.UpdatedOutput = extractUpdatedOutput(upd)
		if ctx, ok := raw["additionalContext"]; ok {
			result.Context = parseContext(ctx)
		}
		return result
	}

	var hso struct {
		PermissionDecision       string          `json:"permissionDecision"`
		PermissionDecisionReason string          `json:"permissionDecisionReason"`
		UpdatedInput             json.RawMessage `json:"updatedInput"`
		UpdatedInput2            json.RawMessage `json:"updated_input"`
	}
	if err := json.Unmarshal(data, &hso); err != nil {
		return HookResult{Decision: DecisionNone}
	}

	result := HookResult{
		Decision: parseDecision(hso.PermissionDecision),
		Reason:   hso.PermissionDecisionReason,
	}

	// Marshal updatedInput back to a string for our opaque format.
	if len(hso.UpdatedInput) > 0 && string(hso.UpdatedInput) != "null" {
		result.UpdatedInput = string(hso.UpdatedInput)
	} else if len(hso.UpdatedInput2) > 0 && string(hso.UpdatedInput2) != "null" {
		result.UpdatedInput = string(hso.UpdatedInput2)
	}

	return result
}

// extractUpdatedOutput extracts distilled string from various hook output shapes:
// - plain string: "distilled"
// - object with stdout: {"stdout": "distilled"}
// - object with content: {"content": "distilled"} or file.content
// - MCP shape: {"status": "success", "result": "distilled"}
// - Host shape with file: {"file": {"content": "distilled"}}
func extractUpdatedOutput(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	// If it's a JSON string, unwrap
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
		return ""
	}
	// If it's an object, try known keys
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return string(raw)
	}
	// Try stdout first (Bash family, most common for omni)
	if v, ok := obj["stdout"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			return s
		}
	}
	// Try content
	if v, ok := obj["content"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			return s
		}
		// content could be array (MCP) -> fallback to extract via content handler
		if s2 := parseContext(v); s2 != "" {
			return s2
		}
	}
	// Try file.content (Read tool)
	if v, ok := obj["file"]; ok {
		var fileObj map[string]json.RawMessage
		if err := json.Unmarshal(v, &fileObj); err == nil {
			if c, ok := fileObj["content"]; ok {
				var s string
				if err := json.Unmarshal(c, &s); err == nil {
					return s
				}
			}
		}
	}
	// Try result (MCP shape)
	if v, ok := obj["result"]; ok {
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			return s
		}
	}
	// Fallback: if object has single string value, return that
	for _, v := range obj {
		var s string
		if err := json.Unmarshal(v, &s); err == nil && s != "" {
			return s
		}
	}
	// Last resort: return raw JSON as string
	return string(raw)
}

// rawToString converts a json.RawMessage to a string suitable for use
// as opaque tool input. It accepts both a JSON object (nested) and a
// JSON string (stringified, for backward compatibility).
func rawToString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	// If it's a JSON string, unwrap it.
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
	}
	// Otherwise it's an object/array — use as-is.
	return string(raw)
}

func parseDecision(s string) Decision {
	switch strings.ToLower(s) {
	case "allow":
		return DecisionAllow
	case "deny":
		return DecisionDeny
	default:
		return DecisionNone
	}
}
