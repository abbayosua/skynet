package tools

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"charm.land/fantasy"
)

//go:embed team.md
var teamDescription string

const TeamToolName = "team"

type TeamParams struct {
	Action     string `json:"action" description:"Action: \"create\", \"list\", \"status\", \"add_member\", \"run\", \"cancel\""`
	TeamName   string `json:"team_name,omitempty" description:"Name of the team (for create/run/status/cancel actions)"`
	Task       string `json:"task,omitempty" description:"The objective for the team (for create/run actions)"`
	MemberID   string `json:"member_id,omitempty" description:"Agent member identifier (for add_member action)"`
	MemberTask string `json:"member_task,omitempty" description:"Task for a specific team member (for add_member action)"`
}

type TeamInfo struct {
	Name      string
	Task      string
	Members   []TeamMemberInfo
	CreatedAt time.Time
}

type TeamMemberInfo struct {
	ID     string
	Task   string
	Status string // "pending", "working", "done", "error"
}

// teamStore holds team state in memory.
var teamStore struct {
	mu    sync.Mutex
	teams map[string]*TeamInfo
}

func init() {
	teamStore.teams = make(map[string]*TeamInfo)
}

// NewTeamTool creates a tool for coordinating multi-agent teams.
// If orchestrator is nil, runs in state-tracking mode (legacy behavior).
func NewTeamTool(workingDir string, orchestrator *TeamOrchestrator) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		TeamToolName,
		teamDescription,
		func(ctx context.Context, params TeamParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			switch params.Action {
			case "create":
				return createTeam(workingDir, params)
			case "list":
				return listTeams()
			case "status":
				return teamStatus(params, orchestrator)
			case "add_member":
				return addTeamMember(params)
			case "run":
				return runTeam(ctx, params, orchestrator)
			case "cancel":
				return cancelTeam(params, orchestrator)
			default:
				return fantasy.NewTextErrorResponse(fmt.Sprintf(
					"unknown action: %q. Supported: create, list, status, add_member, run, cancel", params.Action,
				)), nil
			}
		})
}

func createTeam(workingDir string, params TeamParams) (fantasy.ToolResponse, error) {
	if params.TeamName == "" {
		return fantasy.NewTextErrorResponse("team_name is required"), nil
	}
	if params.Task == "" {
		return fantasy.NewTextErrorResponse("task is required (describe the team's objective)"), nil
	}

	teamStore.mu.Lock()
	defer teamStore.mu.Unlock()

	if _, exists := teamStore.teams[params.TeamName]; exists {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("team %q already exists", params.TeamName)), nil
	}

	team := &TeamInfo{
		Name:      params.TeamName,
		Task:      params.Task,
		Members:   make([]TeamMemberInfo, 0),
		CreatedAt: time.Now(),
	}
	teamStore.teams[params.TeamName] = team

	// Also create a file for persistence.
	teamDir := filepath.Join(workingDir, ".omo", "teams")
	if err := os.MkdirAll(teamDir, 0o755); err == nil {
		content := fmt.Sprintf(`# Team: %s

Created: %s
Task: %s

## Members

None yet. Use `+"`team`"+` with action="add_member" to add team members.

## Execution

Use `+"`team`"+` with action="run" to execute the team workflow.

`,
			params.TeamName,
			time.Now().Format("2006-01-02 15:04:05"),
			params.Task,
		)
		os.WriteFile(filepath.Join(teamDir, params.TeamName+".md"), []byte(content), 0o644)
	}

	return fantasy.NewTextResponse(fmt.Sprintf(
		"## Team Created: %s\n\n**Task**: %s\n\n"+
			"Use `team` with action=\"add_member\" to add team members.\n"+
			"Each member can be assigned a specific sub-task.\n"+
			"Use `team` with action=\"run\" to execute the team workflow.\n"+
			"Use `team` with action=\"status\" to check team progress.",
		params.TeamName, params.Task,
	)), nil
}

func listTeams() (fantasy.ToolResponse, error) {
	teamStore.mu.Lock()
	defer teamStore.mu.Unlock()

	if len(teamStore.teams) == 0 {
		return fantasy.NewTextResponse("No teams created yet. Use `team` with action=\"create\" to form a team."), nil
	}

	var lines []string
	lines = append(lines, "## Teams\n")
	for _, team := range teamStore.teams {
		doneCount := 0
		for _, m := range team.Members {
			if m.Status == "done" {
				doneCount++
			}
		}
		// Check if there's an active execution.
		exec := globalTeamExecStore.execs[team.Name]
		phaseInfo := ""
		if exec != nil && exec.Phase != "idle" && exec.Phase != "completed" && exec.Phase != "failed" {
			phaseInfo = fmt.Sprintf(" [%s, iter %d/%d]", exec.Phase, exec.Iteration, exec.MaxIter)
		}
		lines = append(lines, fmt.Sprintf("- **%s**: %d members (%d done)%s — %s",
			team.Name, len(team.Members), doneCount, phaseInfo, team.Task))
	}

	return fantasy.NewTextResponse(strings.Join(lines, "\n")), nil
}

func teamStatus(params TeamParams, orchestrator *TeamOrchestrator) (fantasy.ToolResponse, error) {
	if params.TeamName == "" {
		return fantasy.NewTextErrorResponse("team_name is required"), nil
	}

	teamStore.mu.Lock()
	team, ok := teamStore.teams[params.TeamName]
	teamStore.mu.Unlock()

	if !ok {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("team %q not found", params.TeamName)), nil
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("## Team: %s\n", team.Name))
	lines = append(lines, fmt.Sprintf("**Task**: %s\n", team.Task))
	lines = append(lines, fmt.Sprintf("**Created**: %s\n", team.CreatedAt.Format("2006-01-02 15:04:05")))

	// Show execution status if available.
	if orchestrator != nil {
		if exec := orchestrator.GetExecution(params.TeamName); exec != nil {
			lines = append(lines, "\n### Execution Status\n")
			lines = append(lines, fmt.Sprintf("**Phase**: %s\n", exec.Phase))
			lines = append(lines, fmt.Sprintf("**Iteration**: %d/%d\n", exec.Iteration, exec.MaxIter))
			lines = append(lines, fmt.Sprintf("**Progress**: %d%%\n", exec.PhaseProgress()))
			if exec.Error != "" {
				lines = append(lines, fmt.Sprintf("**Error**: %s\n", exec.Error))
			}
			if exec.Plan != "" {
				lines = append(lines, "\n**Plan**:\n")
				lines = append(lines, fmt.Sprintf("```\n%s\n```\n", truncateForDisplay(exec.Plan, 400)))
			}
			if exec.Review != "" {
				lines = append(lines, "\n**Review Feedback**:\n")
				lines = append(lines, fmt.Sprintf("```\n%s\n```\n", truncateForDisplay(exec.Review, 300)))
			}
		}
	}

	lines = append(lines, "\n### Members\n")
	if len(team.Members) == 0 {
		lines = append(lines, "No members yet. Use `team` with action=\"add_member\" to add team members.")
	} else {
		for _, m := range team.Members {
			statusIcon := map[string]string{
				"pending": "⏳",
				"working": "🔄",
				"done":    "✅",
				"error":   "❌",
			}[m.Status]
			if statusIcon == "" {
				statusIcon = "❓"
			}
			lines = append(lines, fmt.Sprintf("- %s **%s**: %s — %s", statusIcon, m.ID, m.Task, m.Status))
		}
	}

	lines = append(lines, "\n### Commands\n")
	lines = append(lines, "- `{\"action\": \"run\", \"team_name\": \"...\"}` — Execute the team workflow")
	lines = append(lines, "- `{\"action\": \"add_member\", \"team_name\": \"...\", \"member_id\": \"...\", \"member_task\": \"...\"}` — Add a member")
	lines = append(lines, "- `{\"action\": \"cancel\", \"team_name\": \"...\"}` — Cancel running execution")

	return fantasy.NewTextResponse(strings.Join(lines, "\n")), nil
}

func addTeamMember(params TeamParams) (fantasy.ToolResponse, error) {
	if params.TeamName == "" {
		return fantasy.NewTextErrorResponse("team_name is required"), nil
	}
	if params.MemberID == "" {
		return fantasy.NewTextErrorResponse("member_id is required (e.g. \"planner\", \"implementer\", \"reviewer\", \"tester\")"), nil
	}

	teamStore.mu.Lock()
	defer teamStore.mu.Unlock()

	team, ok := teamStore.teams[params.TeamName]
	if !ok {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("team %q not found. Create it first with action=\"create\".", params.TeamName)), nil
	}

	member := TeamMemberInfo{
		ID:     params.MemberID,
		Task:   params.MemberTask,
		Status: "pending",
	}
	if member.Task == "" {
		member.Task = "Work on team objective"
	}
	team.Members = append(team.Members, member)

	return fantasy.NewTextResponse(fmt.Sprintf(
		"## Member Added: %s\n\n**Team**: %s\n**Task**: %s\n\n"+
			"The member is now part of the team.\n"+
			"Use `team` with action=\"run\" to start executing.\n"+
			"Use `team` with action=\"status\" to see all members.",
		params.MemberID, params.TeamName, member.Task,
	)), nil
}

// runTeam executes the full team workflow.
func runTeam(ctx context.Context, params TeamParams, orchestrator *TeamOrchestrator) (fantasy.ToolResponse, error) {
	if orchestrator == nil {
		return fantasy.NewTextErrorResponse(
			"Team orchestration is not available. The coordinator must provide an agent runner.",
		), nil
	}
	if params.TeamName == "" {
		return fantasy.NewTextErrorResponse("team_name is required"), nil
	}

	teamStore.mu.Lock()
	team, ok := teamStore.teams[params.TeamName]
	teamStore.mu.Unlock()

	if !ok {
		return fantasy.NewTextErrorResponse(fmt.Sprintf("team %q not found. Create it first with action=\"create\".", params.TeamName)), nil
	}

	// Update task if provided.
	if params.Task != "" {
		team.Task = params.Task
	}

	// Check if already running.
	if existing := orchestrator.GetExecution(params.TeamName); existing != nil {
		if existing.Phase == PhaseExecuting || existing.Phase == PhasePlanning || existing.Phase == PhaseReviewing || existing.Phase == PhaseIterating {
			return fantasy.NewTextErrorResponse(fmt.Sprintf(
				"Team %q is already running (phase: %s, iteration: %d/%d). Use action=\"cancel\" to stop it first.",
				params.TeamName, existing.Phase, existing.Iteration, existing.MaxIter,
			)), nil
		}
	}

	// Mark all members as working.
	teamStore.mu.Lock()
	for i := range team.Members {
		team.Members[i].Status = "working"
	}
	teamStore.mu.Unlock()

	// Run the workflow asynchronously if context allows (or synchronously for now).
	// The coordinator's runFn is synchronous, so this blocks. For long tasks,
	// the LLM should use spawn_agent for truly async team execution.
	exec, err := orchestrator.RunTeam(ctx, params.TeamName, team.Task, team.Members)

	// Update member statuses based on results.
	teamStore.mu.Lock()
	if exec != nil {
		for i := range team.Members {
			memberID := team.Members[i].ID
			if _, hasResult := exec.Results[memberID]; hasResult {
				team.Members[i].Status = "done"
			} else if exec.Phase == PhaseFailed || exec.Phase == PhaseCancelled {
				team.Members[i].Status = "error"
			}
		}
	}
	teamStore.mu.Unlock()

	if err != nil {
		return fantasy.NewTextErrorResponse(fmt.Sprintf(
			"Team execution failed: %s\n\n%s", err, exec.FormatStatus(),
		)), nil
	}

	return fantasy.NewTextResponse(exec.FormatStatus()), nil
}

// cancelTeam cancels a running team workflow.
func cancelTeam(params TeamParams, orchestrator *TeamOrchestrator) (fantasy.ToolResponse, error) {
	if params.TeamName == "" {
		return fantasy.NewTextErrorResponse("team_name is required"), nil
	}
	if orchestrator == nil {
		return fantasy.NewTextErrorResponse("Team orchestration is not available."), nil
	}

	orchestrator.CancelTeam(params.TeamName)

	teamStore.mu.Lock()
	for i := range teamStore.teams[params.TeamName].Members {
		teamStore.teams[params.TeamName].Members[i].Status = "pending"
	}
	teamStore.mu.Unlock()

	return fantasy.NewTextResponse(fmt.Sprintf(
		"## Team Cancelled\n\nTeam %q has been cancelled.", params.TeamName,
	)), nil
}
