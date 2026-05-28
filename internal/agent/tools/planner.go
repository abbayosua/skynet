package tools

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/fantasy"
)

//go:embed planner.md
var plannerDescription string

const PlannerToolName = "planner"

// PlannerParams defines the input for the planner tool.
type PlannerParams struct {
	Action        string `json:"action" description:"Action to perform: \"create\" to create a new plan, \"list\" to list existing plans"`
	Task          string `json:"task,omitempty" description:"The task or feature description to plan for (required for create action)"`
	PlanName      string `json:"plan_name,omitempty" description:"A short name for the plan file (e.g. 'auth-system'). Auto-generated from task if not provided."`
	Context       string `json:"context,omitempty" description:"Additional context about the codebase, constraints, or preferences"`
	Acceptance    string `json:"acceptance,omitempty" description:"Specific acceptance criteria the plan should address"`
}

// PlanEntry represents a single task within a plan.
type PlanEntry struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Files       string `json:"files,omitempty"`
	Status      string `json:"status"`
}

// Plan represents a complete work plan.
type Plan struct {
	Name        string      `json:"name"`
	Objective   string      `json:"objective"`
	Scope       string      `json:"scope"`
	Approach    string      `json:"approach"`
	Tasks       []PlanEntry `json:"tasks"`
	CreatedAt   string      `json:"created_at"`
	FilePath    string      `json:"file_path"`
}

func NewPlannerTool(workingDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		PlannerToolName,
		plannerDescription,
		func(ctx context.Context, params PlannerParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			ReportActivity(ctx, "Planning...")
			switch params.Action {
			case "list":
				return listPlans(workingDir)
			case "create":
				return createPlan(workingDir, params)
			default:
				return fantasy.NewTextErrorResponse(fmt.Sprintf(
					"unknown action: %q. Supported actions: \"create\", \"list\"",
					params.Action,
				)), nil
			}
		})
}

// plansDir returns the path to the .omo/plans directory.
func plansDir(workingDir string) string {
	return filepath.Join(workingDir, ".omo", "plans")
}

func listPlans(workingDir string) (fantasy.ToolResponse, error) {
	dir := plansDir(workingDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return fantasy.NewTextResponse("No plans found. Use `planner` with action=\"create\" to create your first plan."), nil
		}
		return fantasy.ToolResponse{}, fmt.Errorf("failed to list plans: %w", err)
	}

	if len(entries) == 0 {
		return fantasy.NewTextResponse("No plans found in .omo/plans/. Use `planner` with action=\"create\" to create a plan."), nil
	}

	var planList []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			info, err := entry.Info()
			if err != nil {
				continue
			}
			planList = append(planList, fmt.Sprintf("- %s (%s)", entry.Name(), info.ModTime().Format("2006-01-02 15:04")))
		}
	}

	if len(planList) == 0 {
		return fantasy.NewTextResponse("No .md plan files found in .omo/plans/."), nil
	}

	return fantasy.NewTextResponse(
		fmt.Sprintf("## Plans in .omo/plans/\n\n%s\n\nUse `view` to read a plan file, or `planner` with action=\"create\" to create a new one.",
			strings.Join(planList, "\n")),
	), nil
}

func createPlan(workingDir string, params PlannerParams) (fantasy.ToolResponse, error) {
	if params.Task == "" {
		return fantasy.NewTextErrorResponse("task is required for create action. Describe what you want to build."), nil
	}

	// Determine plan name.
	planName := params.PlanName
	if planName == "" {
		planName = sanitizePlanName(params.Task)
	}
	planName = strings.TrimSpace(planName)
	if planName == "" {
		planName = fmt.Sprintf("plan-%d", time.Now().Unix())
	}

	// Ensure .omo/plans directory exists.
	dir := plansDir(workingDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fantasy.ToolResponse{}, fmt.Errorf("failed to create plans directory: %w", err)
	}

	// Build the plan file path (avoid overwriting).
	planPath := filepath.Join(dir, planName+".md")
	if _, err := os.Stat(planPath); err == nil {
		planPath = filepath.Join(dir, fmt.Sprintf("%s-%d.md", planName, time.Now().Unix()))
	}

	// Build the context sections.
	contextBlock := ""
	if params.Context != "" {
		contextBlock = fmt.Sprintf("## Context\n\n%s\n\n", params.Context)
	}
	acceptanceBlock := ""
	if params.Acceptance != "" {
		acceptanceBlock = fmt.Sprintf("## Acceptance Criteria\n\n%s\n\n", params.Acceptance)
	}

	// Generate a structured plan template with prompts for the agent to fill.
	planContent := fmt.Sprintf(`# Plan: %s

> Created: %s
> **Status**: Draft

## Objective

%s

## Scope

**In Scope:**
-

**Out of Scope:**
-

%s%s## Approach

-

## Tasks

| # | Task | Files | Status |
|---|------|-------|--------|
| 1 | - | - | pending |

## Risks & Mitigations

-

## Verification

- [ ] All tasks completed
- [ ] Tests pass
- [ ] Edge cases handled
`,
		planName,
		time.Now().Format("2006-01-02 15:04:05"),
		params.Task,
		contextBlock,
		acceptanceBlock,
	)

	if err := os.WriteFile(planPath, []byte(planContent), 0o644); err != nil {
		return fantasy.ToolResponse{}, fmt.Errorf("failed to write plan file: %w", err)
	}

	return fantasy.NewTextResponse(fmt.Sprintf(
		"## Plan Created\n\n**Plan**: %s\n**Path**: `%s`\n\nUse `view` to read the plan, then `edit` or `hashline_edit` to fill in the details.\nWhen ready, run tasks and mark them complete by editing the plan file.\n\n```\n%s\n```",
		planName, planPath, planContent,
	)), nil
}

// sanitizePlanName converts a task description into a safe filename.
func sanitizePlanName(task string) string {
	// Take first ~40 chars, lowercase, replace non-alphanumeric with hyphens.
	cleaned := strings.ToLower(task)
	if len(cleaned) > 40 {
		cleaned = cleaned[:40]
	}

	var result []rune
	for _, r := range cleaned {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result = append(result, r)
		} else if r == ' ' || r == '_' {
			result = append(result, '-')
		}
	}

	name := strings.Trim(string(result), "-")
	if name == "" {
		name = "plan"
	}
	return name
}
