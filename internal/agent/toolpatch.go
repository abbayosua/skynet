package agent

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"charm.land/fantasy"
)

func patchToolSchemas(tools []fantasy.AgentTool, providerID, modelID string) []fantasy.AgentTool {
	if !strings.HasPrefix(providerID, "opencode") && providerID != "MuseV1Responses" {
		return tools
	}
	hasReqHint := strings.Contains(strings.ToLower(modelID), "muse")
	patched := make([]fantasy.AgentTool, len(tools))
	for i, tool := range tools {
		patched[i] = &noRequiredTool{inner: tool, hasReqHint: hasReqHint}
	}
	return patched
}

type noRequiredTool struct {
	inner      fantasy.AgentTool
	hasReqHint bool
}

func (t *noRequiredTool) Info() fantasy.ToolInfo {
	info := t.inner.Info()
	info.Required = []string{}
	if t.hasReqHint && info.Parameters != nil {
		newParams := make(map[string]any, len(info.Parameters))
		for k, v := range info.Parameters {
			if obj, ok := v.(map[string]any); ok {
				if desc, ok := obj["description"].(string); ok && desc != "" && !strings.HasPrefix(desc, "[REQ] ") {
					obj["description"] = "[REQ] " + desc
				}
				newParams[k] = obj
			} else {
				newParams[k] = v
			}
		}
		info.Parameters = newParams
	}
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
		if len(raw) == origLen {
			// No structural change from unwrap (e.g. truncated multi-layer
			// Muse wrapper): try last-resort field salvage before giving up.
			if salvaged := salvageRequiredFields(input, info); salvaged != "" {
				return salvaged
			}
			// Bash fallback independent of unwrap: a healthy-but-incomplete
			// input like {"description": "..."} has no wrapper damage,
			// so nothing above fires — yet the tool would fail with
			// "missing command". Fill command from description instead.
			// Only fires when command is actually missing, so healthy
			// inputs stay byte-identical (prefix cache safe).
			if info.Name == "bash" && !isNonEmptyString(raw["command"]) {
				if v, ok := raw["description"]; ok {
					var s string
					if err := json.Unmarshal(v, &s); err == nil && strings.TrimSpace(s) != "" {
						if b, err := json.Marshal(s); err == nil {
							raw["command"] = b
							if out, err := json.Marshal(raw); err == nil {
								return string(out)
							}
						}
					}
				}
				if m := extractJSONField(input, "command"); m != "" {
					if b, err := json.Marshal(m); err == nil {
						raw["command"] = b
						if out, err := json.Marshal(raw); err == nil {
							return string(out)
						}
					}
				} else if m := extractJSONField(input, "description"); m != "" {
					if b, err := json.Marshal(m); err == nil {
						raw["command"] = b
						if _, ok := raw["description"]; !ok {
							raw["description"] = b
						}
						if out, err := json.Marshal(raw); err == nil {
							return string(out)
						}
					}
				}
			}
		}
		if len(raw) != origLen {
			if info.Name == "bash" {
				changed := false
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
				if changed {
					if b, err := json.Marshal(raw); err == nil {
						return string(b)
					}
				}
			}
			if b, err := json.Marshal(raw); err == nil {
				return string(b)
			}
		}
		if info.Name == "bash" {
			changed := false
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
			if changed {
				if b, err := json.Marshal(raw); err == nil {
					return string(b)
				}
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
		// Last resort: cross-layer salvage. Deeply nested Muse wrappers
		// (arguments-in-arguments, sometimes truncated) can still contain
		// the required fields as plain text. Extract them so the tool can
		// run instead of failing with "field is required".
		salvaged := salvageRequiredFields(input, info)
		if salvaged != "" {
			return salvaged
		}
		return ""
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	return string(b)
}

// salvageRequiredFields extracts required tool fields from a deeply
// nested or truncated wrapper payload where normal JSON parsing fails.
// It unescapes layer by layer, then regex-scans for "field": "value"
// pairs. Only fires when at least one required field is found, so
// healthy inputs are never rewritten (prefix cache stays identical).
func salvageRequiredFields(input string, info fantasy.ToolInfo) string {
	// Strict mode: only fire when the tool's primary field is found.
	// A partial salvage (e.g. description without command) would mask
	// the bash-specific repair below, so require the key field.
	var keyField string
	fields := append([]string{}, info.Required...)
	if len(fields) == 0 {
		switch info.Name {
		case "bash":
			keyField = "command"
			fields = []string{"command", "description"}
		case "view", "write", "edit", "multiedit":
			keyField = "file_path"
			fields = []string{"file_path"}
		default:
			return ""
		}
	} else {
		keyField = fields[0]
	}
	// Only salvage payloads that actually look wrapped or broken. Healthy
	//-but-incomplete inputs (e.g. bash with command but no description)
	// must fall through to the tool-specific repair below.
	lowered := strings.ToLower(input)
	wrapped := strings.Contains(lowered, "\"arguments\"") || strings.Contains(lowered, "params") || strings.Contains(lowered, "tool_input")
	var probe map[string]json.RawMessage
	validJSON := json.Unmarshal([]byte(strings.TrimSpace(input)), &probe) == nil
	if !wrapped && validJSON {
		return ""
	}
	tmp := input
	for i := 0; i < 10; i++ {
		next := strings.ReplaceAll(tmp, `\\`, `\`)
		next = strings.ReplaceAll(next, `\"`, `"`)
		if next == tmp {
			break
		}
		tmp = next
	}
	for i := 0; i < 10; i++ {
		next := strings.ReplaceAll(tmp, `\"`, `"`)
		if next == tmp {
			break
		}
		tmp = next
	}

	out := map[string]string{}
	for _, f := range fields {
		re := regexp.MustCompile(`"` + f + `"\s*:\s*"((?:[^"\\]|\\.)*)"`)
		if m := re.FindStringSubmatch(tmp); m != nil {
			val := strings.ReplaceAll(m[1], `\"`, `"`)
			val = strings.ReplaceAll(val, `\\`, `\`)
			if strings.TrimSpace(val) != "" {
				out[f] = val
			}
		}
	}
	if len(out) == 0 {
		return ""
	}
	if _, ok := out[keyField]; !ok {
		return ""
	}

	b, err := json.Marshal(out)
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
						if tmp[j] == '"' && (j == 0 || tmp[j-1] != '\\') {
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
		return false
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
