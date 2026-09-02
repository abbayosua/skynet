package agent

import (
	"context"
	"encoding/json"
	"strings"

	"charm.land/fantasy"
)

func repairToolCallArgs(_ context.Context, opts fantasy.ToolCallRepairOptions) (*fantasy.ToolCallContent, error) {
	input := opts.OriginalToolCall.Input
	if input == "" {
		return nil, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(input), &raw); err != nil {
		return nil, nil
	}
	wrapperKeys := []string{"arguments", "params", "parameters", "input", "tool_input", "toolInput"}
	unwrapped := false
	if len(raw) == 1 {
		for _, wk := range wrapperKeys {
			if v, ok := raw[wk]; ok {
				var obj map[string]json.RawMessage
				if err := json.Unmarshal(v, &obj); err == nil {
					raw = obj
					unwrapped = true
					break
				}
				var asStr string
				if err := json.Unmarshal(v, &asStr); err == nil {
					var inner map[string]json.RawMessage
					if err := json.Unmarshal([]byte(asStr), &inner); err == nil {
						raw = inner
						unwrapped = true
						break
					}
				}
			}
		}
	}
	if !unwrapped {
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
				if len(merged) != len(raw) {
					raw = merged
					unwrapped = true
				}
			}
		}
	}
	var required []string
	var toolName string
	toolName = opts.OriginalToolCall.ToolName
	for _, t := range opts.AvailableTools {
		if t.Info().Name == toolName {
			required = t.Info().Required
			break
		}
	}
	aliasFixed := false
	if len(required) > 0 {
		normalized := make(map[string]string, len(raw))
		for k := range raw {
			norm := strings.ToLower(strings.ReplaceAll(k, "_", ""))
			normalized[norm] = k
		}
		for _, req := range required {
			if _, exists := raw[req]; exists {
				var sval string
				if err := json.Unmarshal(raw[req], &sval); err == nil && strings.TrimSpace(sval) == "" {
				} else {
					continue
				}
			}
			reqNorm := strings.ToLower(strings.ReplaceAll(req, "_", ""))
			if origKey, ok := normalized[reqNorm]; ok && origKey != req {
				raw[req] = raw[origKey]
				aliasFixed = true
				continue
			}
			for _, alias := range requiredFieldAliases(req) {
				aliasNorm := strings.ToLower(strings.ReplaceAll(alias, "_", ""))
				if origKey, ok := normalized[aliasNorm]; ok {
					raw[req] = raw[origKey]
					aliasFixed = true
					break
				}
			}
		}
		if toolName == "bash" {
			hasCmd := isNonEmptyStringRepair(raw["command"])
			hasDesc := isNonEmptyStringRepair(raw["description"])
			if !hasCmd {
				if v, ok := raw["description"]; ok {
					var s string
					if err := json.Unmarshal(v, &s); err == nil && strings.TrimSpace(s) != "" {
						b, _ := json.Marshal(s)
						raw["command"] = b
						aliasFixed = true
						hasCmd = true
					}
				}
				if !hasCmd {
					if m := extractJSONFieldRepair(input, "command"); m != "" {
						b, _ := json.Marshal(m)
						raw["command"] = b
						aliasFixed = true
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
						aliasFixed = true
					} else if m := extractJSONFieldRepair(input, "description"); m != "" {
						b, _ := json.Marshal(m)
						raw["description"] = b
						aliasFixed = true
					}
				} else if m := extractJSONFieldRepair(input, "description"); m != "" {
					b, _ := json.Marshal(m)
					raw["description"] = b
					aliasFixed = true
				}
			}
		}
	}
	if !unwrapped && !aliasFixed {
		return nil, nil
	}
	repaired, err := json.Marshal(raw)
	if err != nil {
		return nil, nil
	}
	if string(repaired) == input {
		return nil, nil
	}
	result := opts.OriginalToolCall
	result.Input = string(repaired)
	return &result, nil
}

func isNonEmptyStringRepair(raw json.RawMessage) bool {
	if raw == nil {
		return false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return true
	}
	return strings.TrimSpace(s) != ""
}

func extractJSONFieldRepair(input, field string) string {
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
	}
	return ""
}

func shouldUseArgsRepair(providerID string) bool {
	if strings.HasPrefix(providerID, "opencode") {
		return true
	}
	// B.ai uses DeepSeek models which sometimes omit required tool params.
// B.ai uses DeepSeek models which sometimes omit required tool params.
	if providerID == "b-ai" || strings.HasPrefix(providerID, "b-ai-") {
		return true
	}
	return false
}
