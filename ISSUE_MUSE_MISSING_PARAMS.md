# Issue: muse-spark-1.2-contributor via opencode-go — Missing Required Parameters

> Created: 2026-08-27
> **Status**: Open — needs fix
> **Model**: `muse-spark-1.2-contributor` via `opencode-go` provider (Responses API)

## Problem

Muse-spark-1.2-contributor via opencode-go frequently skips **required** tool parameters:
- `description` in bash tool → `missing required parameter: description`
- `command` in bash tool → `missing command`
- `file_path` in write/edit tool → `missing required parameter: file_path`

This happens intermittently — model sometimes sends correct params, sometimes skips fields entirely.

## Root Cause Analysis

### Why `required` enforcement doesn't work

1. **fantasy** (LLM abstraction) builds JSON Schema with `required` array at root level:
   ```go
   // agent.go:1003-1007
   inputSchema := map[string]any{
       "type":       "object",
       "properties": info.Parameters,
       "required":   info.Required,
   }
   ```

2. **opencode-go gateway** **rejects** schemas with `required` arrays:
   ```
   Invalid JSON schema: ["prompt"] is not of types "boolean", "object"
   Invalid JSON schema: null is not of type "array"
   ```

3. Current workaround (`internal/agent/toolpatch.go`): strips `required` for opencode-go providers → gateway happy, but model has no enforcement → skips fields.

### Why model skips fields without `required`

Without `required` in schema, model decides which fields to fill based on description alone. Muse-spark is inconsistent — sometimes fills all fields, sometimes skips.

### Why `repairToolCallArgs` doesn't help

`internal/agent/toolrepair.go` handles `{"arguments": {...}}` wrapper case, but the model sometimes sends params WITHOUT the wrapper — just missing fields entirely.

## Existing Workarounds (partial)

1. **`patchToolSchemas`** (`internal/agent/toolpatch.go`): strips `required` from tool schemas for opencode-go providers → prevents gateway rejection
2. **`repairToolCallArgs`** (`internal/agent/toolrepair.go`): unwraps `{"arguments": {...}}` wrapper when model wraps params

## What Needs to Be Fixed

### Option A: Add `required` hint in tool descriptions (recommended)

Since opencode-go rejects `required` in schema, encode it in the **description** field instead. For example:

**Before:**
```
"description": "The command to execute"
```

**After:**
```
"description": "The command to execute. REQUIRED — do not omit this field."
```

This is what opencode CLI does — uses natural language hints instead of formal `required`.

**Where to change:**
- `internal/agent/tools/bash.go` — `BashParams` description tags
- `internal/agent/tools/write.go` — `WriteParams` description tags
- `internal/agent/tools/edit.go` — `EditParams` description tags
- `internal/agent/tools/multiedit.go` — `MultiEditParams` description tags

**Approach:** Add `[REQUIRED]` prefix to critical field descriptions in the Go struct tags:

```go
type BashParams struct {
    Description string `json:"description" description:"[REQUIRED] Brief description of what the command does"`
    Command     string `json:"command" description:"[REQUIRED] The command to execute"`
    // ...
}
```

### Option B: Add default fallback for missing fields

In the tool handler, if a critical field is empty, try to extract it from other fields:

```go
// In bash tool handler
if params.Command == "" && params.Description != "" {
    // Try to extract command from description
    // e.g., "run: echo hi" → command = "echo hi"
}
```

**Risk:** fragile, may misinterpret descriptions as commands.

### Option C: Pre-fill required fields via repair function

Expand `repairToolCallArgs` in `internal/agent/toolrepair.go` to detect and fill missing required fields:

```go
func repairToolCallArgs(_ context.Context, opts fantasy.ToolCallRepairOptions) (*fantasy.ToolCallContent, error) {
    // ... existing unwrap logic ...

    // NEW: if command is missing but description contains a command hint, extract it
    if _, hasCmd := argumentsObj["command"]; !hasCmd {
        if desc, ok := argumentsObj["description"]; ok {
            // parse description to extract command
        }
    }
}
```

### Option D: Request schema change in opencode-go gateway

Ask opencode-go team to accept `required` arrays in tool schemas. This would be the cleanest fix but requires upstream change.

## Recommended Fix

**Option A + C combined:**
1. Add `[REQUIRED]` prefix to critical field descriptions (Option A)
2. Keep repair function as safety net (Option C)
3. Keep `patchToolSchemas` stripping `required` for gateway compatibility

## Files to Modify

| File | Change |
|------|--------|
| `internal/agent/tools/bash.go` | Add `[REQUIRED]` to `BashParams` description tags |
| `internal/agent/tools/write.go` | Add `[REQUIRED]` to `WriteParams` description tags |
| `internal/agent/tools/edit.go` | Add `[REQUIRED]` to `EditParams` description tags |
| `internal/agent/tools/multiedit.go` | Add `[REQUIRED]` to `MultiEditParams` description tags |
| `internal/agent/tools/view.go` | Add `[REQUIRED]` to `ViewParams` description tags (if applicable) |
| `internal/agent/toolrepair.go` | Expand repair to fill missing fields from description |

## Testing

After fix, test with:
```bash
export OPENCODE_API_KEY=$(python3 -c "import json;print(json.load(open('$HOME/.local/share/skynet/skynet.json'))['providers']['opencode-go']['api_key'])")

# Test bash
~/go/bin/skynet run -q -m opencode-go/muse-spark-1.2-contributor "use bash to run: echo test_pass"

# Test write
~/go/bin/skynet run -q -m opencode-go/muse-spark-1.2-contributor "create a file called /tmp/muse_test.txt with content 'hello muse'"

# Test edit
echo "old content" > /tmp/muse_edit.txt
~/go/bin/skynet run -q -m opencode-go/muse-spark-1.2-contributor "edit /tmp/muse_edit.txt to change 'old content' to 'new content'"

# Test parallel calls
~/go/bin/skynet run -q -m opencode-go/muse-spark-1.2-contributor "use glob to find all .go files in /tmp AND use ls to list files in /tmp — run both in parallel"
```

## Context Files

- `internal/agent/toolpatch.go` — strips `required` for opencode-go
- `internal/agent/toolrepair.go` — unwraps `{"arguments": {...}}` wrapper
- `internal/agent/coordinator.go:910` — calls `patchToolSchemas`
- `internal/agent/agent.go:274-282` — sets up repair function
