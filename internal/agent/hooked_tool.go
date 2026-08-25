package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"charm.land/fantasy"
	"github.com/abbayosua/skynet/internal/agent/tools"
	"github.com/abbayosua/skynet/internal/hooks"
	"github.com/abbayosua/skynet/internal/permission"
	"github.com/tidwall/sjson"
)

// hookedTool wraps a fantasy.AgentTool to run PreToolUse hooks before
// and PostToolUse hooks after delegating to the inner tool.
type hookedTool struct {
	inner      fantasy.AgentTool
	preRunner  *hooks.Runner
	postRunner *hooks.Runner
}

func newHookedTool(inner fantasy.AgentTool, preRunner, postRunner *hooks.Runner) *hookedTool {
	return &hookedTool{inner: inner, preRunner: preRunner, postRunner: postRunner}
}

// wrapToolsWithHooks returns a tool slice with each entry wrapped in a
// hookedTool. Returns the original slice unchanged when both runners are nil or
// when isSubAgent is true — sub-agents never fire hooks, the top-level
// invocation of the sub-agent tool itself is wrapped on the caller's side.
func wrapToolsWithHooks(tools []fantasy.AgentTool, preRunner, postRunner *hooks.Runner, isSubAgent bool) []fantasy.AgentTool {
	if (preRunner == nil && postRunner == nil) || isSubAgent {
		return tools
	}
	out := make([]fantasy.AgentTool, len(tools))
	for i, tool := range tools {
		out[i] = newHookedTool(tool, preRunner, postRunner)
	}
	return out
}

func (h *hookedTool) Info() fantasy.ToolInfo {
	return h.inner.Info()
}

func (h *hookedTool) ProviderOptions() fantasy.ProviderOptions {
	return h.inner.ProviderOptions()
}

func (h *hookedTool) SetProviderOptions(opts fantasy.ProviderOptions) {
	h.inner.SetProviderOptions(opts)
}

func (h *hookedTool) Run(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	sessionID := tools.GetSessionFromContext(ctx)
	var preResult hooks.AggregateResult
	var preErr error
	if h.preRunner != nil {
		preResult, preErr = h.preRunner.Run(ctx, hooks.EventPreToolUse, sessionID, call.Name, call.Input)
		if preErr != nil {
			slog.Warn("PreToolUse hook execution error, proceeding with tool call",
				"tool", call.Name, "error", preErr)
		}
		if preResult.Decision == hooks.DecisionDeny || preResult.Halt {
			reason := fmt.Sprintf("Tool call blocked by hook. Reason: %s", preResult.Reason)
			if preResult.Halt {
				reason = fmt.Sprintf("Turn halted by hook. Reason: %s", preResult.Reason)
			}
			resp := fantasy.NewTextErrorResponse(reason)
			resp.StopTurn = preResult.Halt
			resp.Metadata = hookMetadataJSON(preResult)
			return resp, nil
		}
		if preResult.UpdatedInput != "" {
			call.Input = preResult.UpdatedInput
		}
		if preResult.Decision == hooks.DecisionAllow {
			ctx = permission.WithHookApproval(ctx, call.ID)
		}
	}

	resp, err := h.inner.Run(ctx, call)
	if err != nil {
		return resp, err
	}

	// Merge Pre context before Post so order is deterministic (Pre then Post)
	if preResult.Context != "" {
		if resp.Content != "" {
			resp.Content += "\n"
		}
		resp.Content += preResult.Context
	}

	// Run PostToolUse hooks after tool execution (e.g. omni distillation)
	var postResult hooks.AggregateResult
	if h.postRunner != nil {
		// Use original Input (potentially rewritten by Pre) as tool_input for Post
		postResult, err = h.postRunner.RunPost(ctx, sessionID, call.Name, call.Input, resp.Content, resp.IsError)
		if err != nil {
			slog.Warn("PostToolUse hook execution error, proceeding with original output",
				"tool", call.Name, "error", err)
		} else {
			if postResult.UpdatedOutput != "" {
				resp.Content = postResult.UpdatedOutput
			}
			if postResult.Context != "" {
				if resp.Content != "" {
					resp.Content += "\n"
				}
				resp.Content += postResult.Context
			}
		}
	}

	// Merge hook metadata from both Pre and Post
	combined := preResult
	if h.postRunner != nil && postResult.HookCount > 0 {
		// Combine counts and hooks for UI display
		combined.HookCount += postResult.HookCount
		combined.Hooks = append(combined.Hooks, postResult.Hooks...)
		if postResult.UpdatedOutput != "" {
			combined.UpdatedOutput = postResult.UpdatedOutput
		}
		if postResult.Context != "" {
			if combined.Context != "" {
				combined.Context += "\n"
			}
			combined.Context += postResult.Context
		}
		// Prefer Post decision if it denies? For Post, deny is not typical, but keep Pre's decision
		if postResult.Decision == hooks.DecisionDeny {
			combined.Decision = postResult.Decision
			combined.Reason = postResult.Reason
		}
		if postResult.Halt {
			combined.Halt = true
			combined.Reason = postResult.Reason
		}
	}
	resp.Metadata = mergeHookMetadata(resp.Metadata, combined)
	return resp, nil
}

// buildHookMetadata creates a HookMetadata from an AggregateResult.
func buildHookMetadata(result hooks.AggregateResult) hooks.HookMetadata {
	return hooks.HookMetadata{
		HookCount:     result.HookCount,
		Decision:      result.Decision.String(),
		Halt:          result.Halt,
		Reason:        result.Reason,
		InputRewrite:  result.UpdatedInput != "",
		OutputRewrite: result.UpdatedOutput != "",
		Hooks:         result.Hooks,
	}
}

// hookMetadataJSON builds a JSON string containing only the hook metadata.
func hookMetadataJSON(result hooks.AggregateResult) string {
	meta := buildHookMetadata(result)
	data, err := json.Marshal(meta)
	if err != nil {
		return ""
	}
	return `{"hook":` + string(data) + `}`
}

// mergeHookMetadata injects hook metadata into existing tool metadata.
func mergeHookMetadata(existing string, result hooks.AggregateResult) string {
	if result.HookCount == 0 {
		return existing
	}
	meta := buildHookMetadata(result)
	data, err := json.Marshal(meta)
	if err != nil {
		return existing
	}
	if existing == "" {
		existing = "{}"
	}
	merged, err := sjson.SetRaw(existing, "hook", string(data))
	if err != nil {
		return existing
	}
	return merged
}
