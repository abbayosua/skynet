package tools

import (
	"context"
	_ "embed"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/fantasy"
)

const SleepToolName = "sleep"

//go:embed sleep.md
var sleepDescription string

type SleepParams struct {
	Duration string  `json:"duration,omitempty" description:"Duration to sleep, e.g. \"30s\", \"1.5s\", \"2m\". Supports Go duration strings. If numeric without unit, treated as seconds"`
	Seconds  float64 `json:"seconds,omitempty" description:"Alternative: seconds to sleep as number (e.g. 30, 1.5). Used if duration not provided"`
}

type SleepResponseMetadata struct {
	Duration string  `json:"duration"`
	Seconds  float64 `json:"seconds"`
	SleptMs  int64   `json:"slept_ms"`
}

func NewSleepTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(
		SleepToolName,
		sleepDescription,
		func(ctx context.Context, params SleepParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			d, err := parseSleepDuration(params)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}

			// Enforce max to avoid hanging agent for too long.
			const maxDuration = 300 * time.Second
			if d > maxDuration {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("duration %s exceeds maximum 300s", d)), nil
			}
			if d <= 0 {
				return fantasy.NewTextErrorResponse("duration must be > 0"), nil
			}

			ReportActivity(ctx, fmt.Sprintf("Sleeping %s", d))

			start := time.Now()
			select {
			case <-time.After(d):
			case <-ctx.Done():
				return fantasy.ToolResponse{}, ctx.Err()
			}
			elapsed := time.Since(start)

			metadata := SleepResponseMetadata{
				Duration: d.String(),
				Seconds:  d.Seconds(),
				SleptMs:  elapsed.Milliseconds(),
			}

			msg := fmt.Sprintf("Slept for %s (%.3f seconds)", d, d.Seconds())
			return fantasy.WithResponseMetadata(fantasy.NewTextResponse(msg), metadata), nil
		})
}

func parseSleepDuration(params SleepParams) (time.Duration, error) {
	// Prefer Duration string if provided.
	if strings.TrimSpace(params.Duration) != "" {
		raw := strings.TrimSpace(params.Duration)
		// If purely numeric (e.g. "30" or "1.5"), treat as seconds.
		if isNumeric(raw) {
			f, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid duration %q: %w", raw, err)
			}
			return time.Duration(f * float64(time.Second)), nil
		}
		d, err := time.ParseDuration(raw)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q: %w", raw, err)
		}
		return d, nil
	}
	if params.Seconds != 0 {
		return time.Duration(params.Seconds * float64(time.Second)), nil
	}
	return 0, fmt.Errorf("missing duration: provide duration (e.g. \"30s\") or seconds (e.g. 30)")
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}
