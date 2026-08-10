# Bedrock Provider Fix

## Problem

Skynet v0.1.2 crashes on startup with:

```
Failed to configure providers: bedrock provider only supports anthropic models for now, found: us.anthropic.claude-sonnet-5.
```

## Root Cause

Two issues:

1. **Remote providers.json incompatible with validation**: Skynet auto-fetches provider list from `catwalk.charm.land` on every startup. The remote `providers.json` contains AWS Bedrock models with region-prefixed IDs (`us.anthropic.claude-sonnet-5`, `eu.anthropic.claude-sonnet-5`, etc.). The embedded providers (bundled in the binary at build time) use the older format without region prefix (`anthropic.claude-sonnet-5`).

2. **Validation too strict**: In `internal/config/load.go:314`, the bedrock provider configuration validates model IDs with:
   ```go
   if !strings.HasPrefix(model.ID, "anthropic.") {
       return fmt.Errorf("bedrock provider only supports anthropic models for now, found: %s", model.ID)
   }
   ```
   This rejects valid AWS Bedrock model IDs that include the region prefix (`us.` or `eu.`).

## Fix

**File**: `internal/config/load.go` (lines 313-317)

**Before**:
```go
for _, model := range p.Models {
    if !strings.HasPrefix(model.ID, "anthropic.") {
        return fmt.Errorf("bedrock provider only supports anthropic models for now, found: %s", model.ID)
    }
}
```

**After**:
```go
for _, model := range p.Models {
    id := strings.TrimPrefix(model.ID, "us.")
    id = strings.TrimPrefix(id, "eu.")
    if !strings.HasPrefix(id, "anthropic.") {
        return fmt.Errorf("bedrock provider only supports anthropic models for now, found: %s", model.ID)
    }
}
```

This strips the region prefix (`us.` or `eu.`) before checking if the model ID starts with `anthropic.`, allowing both old and new AWS Bedrock model ID formats.

## Build

```bash
cd /Users/user/Documents/skynet
go build -o /Users/user/go/bin/skynet .
```

## Notes

- The `CRUSH_DISABLE_PROVIDER_AUTO_UPDATE=1` env var and `disable_provider_auto_update` config field are ignored by the catwalk module — it always fetches and caches the remote providers.json on startup.
- Even with the fix, skynet will overwrite `providers.json` with the remote version every startup. That's fine now since the validation accepts both formats.
