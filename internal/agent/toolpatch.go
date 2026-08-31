package agent

import (
	"context"
	"encoding/json"
	"strings"

	"charm.land/fantasy"
)

func patchToolSchemas(tools []fantasy.AgentTool, providerID string) []fantasy.AgentTool {
	if !strings.HasPrefix(providerID, "opencode") {
		return tools
	}
	patched := make([]fantasy.AgentTool, len(tools))
	for i, tool := range tools {
		patched[i] = &noRequiredTool{inner: tool}
	}
	return patched
}

type noRequiredTool struct {
	inner fantasy.AgentTool
}

func (t *noRequiredTool) Info() fantasy.ToolInfo {
	info := t.inner.Info()
	info.Required = []string{}
	return info
}

func (t *noRequiredTool) Run(ctx context.Context, params fantasy.ToolCall) (fantasy.ToolResponse, error) {
	if fixed := repairOpencodeInput(params.Input, t.inner.Info()); fixed != "" && fixed != params.Input {
		params.Input = fixed
	}
	return t.inner.Run(ctx, params)
}

func repairOpencodeInput(input string, info fantasy.ToolInfo) string {
	if input == "" {
		return ""
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(input), &raw); err != nil {
		return ""
	}
	origLen := len(raw)
	origRawJSON, _ := json.Marshal(raw)
	// --- 1) unwrap wrappers recursively ---
	raw = unwrapWrappersRecursively(raw)
	changedByUnwrap := false
	if newJSON, err := json.Marshal(raw); err == nil {
		changedByUnwrap = string(newJSON) != string(origRawJSON)
	}
	// Multi-key merge where "arguments" coexists
	if _, hasWrapper := raw["arguments"]; hasWrapper && len(raw) > 1 {
		if v, ok := raw["arguments"]; ok {
			var wrapperObj map[string]json.RawMessage
			if err := json.Unmarshal(v, &wrapperObj); err == nil {
				merged := make(map[string]json.RawMessage, len(raw)+len(wrapperObj))
				for k, val := range raw {
					if k == "arguments" {
						continue
					}
					merged[k] = val
				}
				for k, val := range wrapperObj {
					if _, exists := merged[k]; !exists {
						merged[k] = val
					}
				}
				if len(merged) > len(raw) {
					raw = merged
					changedByUnwrap = true
				}
			}
		}
	}
	if len(info.Required) == 0 {
		if len(raw) != origLen {
			if b, err := json.Marshal(raw); err == nil {
				return string(b)
			}
		}
		return ""
	}
	normalized := make(map[string]string, len(raw))
	for k := range raw {
		norm := strings.ToLower(strings.ReplaceAll(k, "_", ""))
		normalized[norm] = k
	}
	changed := changedByUnwrap || len(raw) != origLen
	for _, req := range info.Required {
		if _, exists := raw[req]; exists {
			var sval string
			if err := json.Unmarshal(raw[req], &sval); err == nil && strings.TrimSpace(sval) == "" {
			} else {
				continue
			}
		}
		reqNorm := strings.ToLower(strings.ReplaceAll(req, "_", ""))
		if origKey, ok := normalized[reqNorm]; ok {
			if origKey != req {
				raw[req] = raw[origKey]
				changed = true
			}
			continue
		}
		aliases := requiredFieldAliases(req)
		for _, alias := range aliases {
			aliasNorm := strings.ToLower(strings.ReplaceAll(alias, "_", ""))
			if origKey, ok := normalized[aliasNorm]; ok {
				raw[req] = raw[origKey]
				changed = true
				break
			}
		}
	}
	if info.Name == "bash" {
		hasCmd := isNonEmptyString(raw["command"])
		hasDesc := isNonEmptyString(raw["description"])
		if !hasCmd {
			if v, ok := raw["description"]; ok {
				var s string
				if err := json.Unmarshal(v, &s); err == nil && strings.TrimSpace(s) != "" {
					b, _ := json.Marshal(s)
					raw["command"] = b
					changed = true
					hasCmd = true
				}
			}
			if !hasCmd {
				if m := extractJSONField(input, "command"); m != "" {
					b, _ := json.Marshal(m)
					raw["command"] = b
					changed = true
				}
			}
		}
		if !hasDesc {
			if v, ok := raw["command"]; ok {
				var s string
				if err := json.Unmarshal(v, &s); err == nil && strings.TrimSpace(s) != "" {
					desc := s
					if len(desc) > 30 {
						desc = desc[:30]
					}
					b, _ := json.Marshal(desc)
					raw["description"] = b
					changed = true
				} else if m := extractJSONField(input, "description"); m != "" {
					b, _ := json.Marshal(m)
					raw["description"] = b
					changed = true
				}
			} else if m := extractJSONField(input, "description"); m != "" {
				b, _ := json.Marshal(m)
				raw["description"] = b
				changed = true
			}
		}
	}
	if !changed {
		return ""
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	return string(b)
}

func extractJSONField(input, field string) string {
	var find func([]byte) string
	find = func(data []byte) string {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(data, &m); err != nil {
			return ""
		}
		if v, ok := m[field]; ok {
			var s string
			if err := json.Unmarshal(v, &s); err == nil && strings.TrimSpace(s) != "" {
				return s
			}
		}
		for _, v := range m {
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(v, &obj); err == nil {
				if res := find(v); res != "" {
					return res
				}
			}
			var s string
			if err := json.Unmarshal(v, &s); err == nil {
				if res := find([]byte(s)); res != "" {
					return res
				}
			}
		}
		return ""
	}
	if res := find([]byte(input)); res != "" {
		return res
	}
	tmp := input
	for i := 0; i < 6; i++ {
		newTmp := strings.ReplaceAll(tmp, "\\\\", "\\")
		newTmp = strings.ReplaceAll(newTmp, "\\\"", "\"")
		if newTmp == tmp {
			break
		}
		tmp = newTmp
		if res := find([]byte(tmp)); res != "" {
			return res
		}
		fieldPattern := `"` + field + `"`
		idx := strings.Index(strings.ToLower(tmp), strings.ToLower(fieldPattern))
		if idx != -1 {
			colonIdx := strings.Index(tmp[idx+len(fieldPattern):], ":")
			if colonIdx != -1 {
				colonPos := idx + len(fieldPattern) + colonIdx
				quoteStart := -1
				for j := colonPos + 1; j < len(tmp); j++ {
					if tmp[j] == '"' {
						quoteStart = j
						break
					}
				}
				if quoteStart != -1 {
					for j := quoteStart + 1; j < len(tmp); j++ {
						if tmp[j] == '"' && (j==0 || tmp[j-1] != '\\') {
							return tmp[quoteStart+1 : j]
						}
					}
				}
			}
		}
	}
	return ""
}

func unwrapWrappersRecursively(raw map[string]json.RawMessage) map[string]json.RawMessage {
	wrapperKeys := []string{"arguments", "params", "parameters", "input", "tool_input", "toolInput", "tool_input_json"}
	for iter := 0; iter < 10; iter++ {
		if len(raw) != 1 {
			break
		}
		var found bool
		var next map[string]json.RawMessage
		for _, wk := range wrapperKeys {
			v, ok := raw[wk]
			if !ok {
				continue
			}
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(v, &obj); err == nil && len(obj) > 0 {
				next = obj
				found = true
				break
			}
			var asStr string
			if err := json.Unmarshal(v, &asStr); err == nil {
				if m, ok := tryParseJSONRecursively(asStr); ok {
					next = m
					found = true
					break
				}
			}
		}
		if !found {
			break
		}
		raw = next
	}
	return raw
}

func tryParseJSONRecursively(s string) (map[string]json.RawMessage, bool) {
	tmp := strings.TrimSpace(s)
	for i := 0; i < 6; i++ {
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(tmp), &m); err == nil && len(m) > 0 {
			return m, true
		}
		var nxt string
		if err := json.Unmarshal([]byte(tmp), &nxt); err != nil {
			break
		}
		tmp = strings.TrimSpace(nxt)
	}
	return nil, false
}

func isNonEmptyString(raw json.RawMessage) bool {
	if raw == nil {
		return false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return true
	}
	return strings.TrimSpace(s) != ""
}

func requiredFieldAliases(field string) []string {
	switch field {
	case "file_path":
		return []string{"filePath", "filepath", "path", "file", "filename", "file_name"}
	case "command":
		return []string{"cmd", "shell_command", "shellCommand", "bash_command", "command_line"}
	case "description":
		return []string{"desc", "description", "summary", "title"}
	case "content":
		return []string{"text", "data", "body", "value", "file_content"}
	case "old_string":
		return []string{"oldString", "old", "search", "old_text", "oldText", "target"}
	case "new_string":
		return []string{"newString", "new", "replace", "new_text", "newText", "replacement"}
	case "pattern":
		return []string{"query", "search", "regex", "glob", "pattern", "filter"}
	case "edits":
		return []string{"edits", "operations", "changes"}
	case "shell_id":
		return []string{"shellId", "shell_id", "id", "job_id"}
	default:
		return nil
	}
}

func (t *noRequiredTool) ProviderOptions() fantasy.ProviderOptions {
	return t.inner.ProviderOptions()
}

func (t *noRequiredTool) SetProviderOptions(opts fantasy.ProviderOptions) {
	t.inner.SetProviderOptions(opts)
}
