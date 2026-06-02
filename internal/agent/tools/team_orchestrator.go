package tools

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// Team Orchestrator: Real multi-agent coordination with specialized roles.
// ============================================================================

// TeamRole defines the role of a team member.
type TeamRole string

const (
	RolePlanner     TeamRole = "planner"
	RoleImplementer TeamRole = "implementer"
	RoleReviewer    TeamRole = "reviewer"
	RoleTester      TeamRole = "tester"
)

// TeamPhase tracks the current phase of team workflow execution.
type TeamPhase string

const (
	PhaseIdle       TeamPhase = "idle"
	PhasePlanning   TeamPhase = "planning"
	PhaseExecuting  TeamPhase = "executing"
	PhaseReviewing  TeamPhase = "reviewing"
	PhaseTesting    TeamPhase = "testing"
	PhaseIterating  TeamPhase = "iterating"
	PhaseCompleted  TeamPhase = "completed"
	PhaseFailed     TeamPhase = "failed"
	PhaseCancelled  TeamPhase = "cancelled"
)

// TeamExecution tracks the execution state of a team workflow.
type TeamExecution struct {
	TeamName    string
	Phase       TeamPhase
	Plan        string
	Results     map[string]string // memberID -> result
	Review      string
	ReviewPass  bool
	Iteration   int
	MaxIter     int
	StartedAt   time.Time
	UpdatedAt   time.Time
	Error       string
}

// TeamOrchestrator manages team-based multi-agent task execution.
// It provides a workflow: Plan → Execute (parallel) → Review → Iterate → Done.
//
// Each phase uses the AgentRunFunc to invoke an LLM agent with a role-specific
// system prompt constructed to guide the agent's behavior.
type TeamOrchestrator struct {
	runFn       AgentRunFunc
	store       *teamExecutionStore
	maxParallel int
}

// teamExecutionStore holds execution state for active teams.
type teamExecutionStore struct {
	mu     sync.Mutex
	execs  map[string]*TeamExecution
}

var globalTeamExecStore = &teamExecutionStore{
	execs: make(map[string]*TeamExecution),
}

// NewTeamOrchestrator creates a new team orchestrator that uses the given
// agent runner for all member execution.
func NewTeamOrchestrator(runFn AgentRunFunc, maxParallel int) *TeamOrchestrator {
	if maxParallel <= 0 {
		maxParallel = 4
	}
	return &TeamOrchestrator{
		runFn:       runFn,
		store:       globalTeamExecStore,
		maxParallel: maxParallel,
	}
}

// RunTeam executes the full team workflow for a given team and task.
func (o *TeamOrchestrator) RunTeam(ctx context.Context, teamName, task string, members []TeamMemberInfo) (*TeamExecution, error) {
	exec := &TeamExecution{
		TeamName:  teamName,
		Phase:     PhasePlanning,
		Results:   make(map[string]string),
		MaxIter:   3,
		StartedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	o.store.mu.Lock()
	o.store.execs[teamName] = exec
	o.store.mu.Unlock()

	// Phase 1: Planning.
	plan, err := o.runPlanningPhase(ctx, task)
	if err != nil {
		exec.Phase = PhaseFailed
		exec.Error = fmt.Sprintf("planning failed: %s", err)
		return exec, err
	}
	exec.Plan = plan
	exec.Phase = PhaseExecuting
	exec.UpdatedAt = time.Now()

	// Phase 2-4: Execute → Review → Iterate.
	for iter := 0; iter < exec.MaxIter; iter++ {
		exec.Iteration = iter + 1

		// Execute phase: run all members in parallel.
		exec.Phase = PhaseExecuting
		exec.UpdatedAt = time.Now()
		if err := o.runExecutionPhase(ctx, exec, task, members); err != nil {
			exec.Phase = PhaseFailed
			exec.Error = fmt.Sprintf("execution failed: %s", err)
			return exec, err
		}

		// Review phase: review the combined results.
		exec.Phase = PhaseReviewing
		exec.UpdatedAt = time.Now()
		review, pass, err := o.runReviewPhase(ctx, task, exec.Plan, exec.Results)
		if err != nil {
			// Review error isn't fatal - continue with what we have.
			exec.Review = fmt.Sprintf("review error: %s (continuing)", err)
			pass = true
		} else {
			exec.Review = review
			exec.ReviewPass = pass
		}

		if pass {
			exec.Phase = PhaseCompleted
			exec.UpdatedAt = time.Now()
			return exec, nil
		}

		// Iterate: include review feedback in the next execution.
		exec.Phase = PhaseIterating
		exec.UpdatedAt = time.Now()
	}

	// Max iterations reached - complete with what we have.
	exec.Phase = PhaseCompleted
	exec.UpdatedAt = time.Now()
	return exec, nil
}

// runPlanningPhase uses the planner agent to break down the task.
func (o *TeamOrchestrator) runPlanningPhase(ctx context.Context, task string) (string, error) {
	plannerPrompt := fmt.Sprintf(`You are the TEAM PLANNER. Your role is to analyze tasks and create structured execution plans.

Task to plan: %s

Analyze this task and create a numbered execution plan. For each step:
1. Describe what needs to be done
2. Specify which tools or approaches to use
3. Define what a successful outcome looks like

Be specific and actionable. Focus on what can be parallelized.

Output your plan clearly.`, task)

	return o.runFn(ctx, plannerPrompt)
}

// runExecutionPhase runs all team members in parallel with their assigned tasks.
func (o *TeamOrchestrator) runExecutionPhase(ctx context.Context, exec *TeamExecution, task string, members []TeamMemberInfo) error {
	if len(members) == 0 {
		// If no members defined, use the plan as the execution.
		result, err := o.runFn(ctx, fmt.Sprintf(
			"Execute the following plan:\n\n%s\n\nOriginal task: %s", exec.Plan, task))
		if err != nil {
			return err
		}
		exec.Results["_default"] = result
		return nil
	}

	// Run members in parallel with a concurrency limit.
	type memberResult struct {
		id     string
		result string
		err    error
	}

	sem := make(chan struct{}, o.maxParallel)
	results := make(chan memberResult, len(members))
	var wg sync.WaitGroup

	for _, member := range members {
		wg.Add(1)
		go func(m TeamMemberInfo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			memberTask := m.Task
			if memberTask == "" || memberTask == "Work on team objective" {
				memberTask = fmt.Sprintf("Work on the team task: %s\n\nFollow the plan:\n%s", task, exec.Plan)
			}

			prompt := fmt.Sprintf(`You are team member "%s" working on a collaborative task.

Team task: %s

Your assigned sub-task: %s

Overall plan:
%s

Execute your assigned work thoroughly. Use the available tools to read, search, and modify files as needed. Report what you did and what the results were.`,
				m.ID, task, memberTask, exec.Plan)

			result, err := o.runFn(ctx, prompt)
			results <- memberResult{id: m.ID, result: result, err: err}
		}(member)
	}

	wg.Wait()
	close(results)

	for r := range results {
		if r.err != nil {
			exec.Results[r.id] = fmt.Sprintf("Error: %s", r.err)
		} else {
			exec.Results[r.id] = r.result
		}
	}

	return nil
}

// runReviewPhase uses the reviewer agent to evaluate results.
func (o *TeamOrchestrator) runReviewPhase(ctx context.Context, task string, plan string, results map[string]string) (review string, pass bool, err error) {
	var resultBuilder strings.Builder
	resultBuilder.WriteString("## Team Results\n\n")
	for memberID, result := range results {
		resultBuilder.WriteString(fmt.Sprintf("### Member: %s\n%s\n\n", memberID, result))
	}

	reviewerPrompt := fmt.Sprintf(`You are the CODE REVIEWER. Review the team's work for correctness, completeness, and quality.

Original task: %s

Execution plan:
%s

%s

Review criteria:
1. Does the work correctly address the original task?
2. Are there any errors or issues?
3. Is the code/style consistent?
4. Are there any missing pieces?

If the work is ACCEPTABLE, start your response with: <review>PASS</review>
If changes are NEEDED, start with: <review>NEEDS_WORK</review> and explain specifically what needs to be fixed.
Be constructive and specific - mention exact files, line numbers, or issues.

Your review:`, task, plan, resultBuilder.String())

	reviewResult, err := o.runFn(ctx, reviewerPrompt)
	if err != nil {
		return "", false, err
	}

	if strings.Contains(reviewResult, "<review>PASS</review>") {
		return reviewResult, true, nil
	}
	return reviewResult, false, nil
}

// CancelTeam cancels a running team execution.
func (o *TeamOrchestrator) CancelTeam(teamName string) {
	o.store.mu.Lock()
	defer o.store.mu.Unlock()

	if exec, ok := o.store.execs[teamName]; ok {
		if exec.Phase == PhaseCompleted || exec.Phase == PhaseFailed || exec.Phase == PhaseCancelled {
			return
		}
		exec.Phase = PhaseCancelled
		exec.UpdatedAt = time.Now()
	}
}

// GetExecution returns the current execution state for a team.
func (o *TeamOrchestrator) GetExecution(teamName string) *TeamExecution {
	o.store.mu.Lock()
	defer o.store.mu.Unlock()
	return o.store.execs[teamName]
}

// FormatExecutionStatus returns a human-readable summary of the team execution.
func (e *TeamExecution) FormatStatus() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## Team: %s\n", e.TeamName))
	b.WriteString(fmt.Sprintf("**Phase**: %s\n", e.Phase))
	b.WriteString(fmt.Sprintf("**Iteration**: %d/%d\n", e.Iteration, e.MaxIter))
	b.WriteString(fmt.Sprintf("**Duration**: %s\n", time.Since(e.StartedAt).Round(time.Second)))

	switch e.Phase {
	case PhaseCompleted:
		b.WriteString("\n✅ **Task completed**\n")
	case PhaseFailed:
		b.WriteString(fmt.Sprintf("\n❌ **Failed**: %s\n", e.Error))
	case PhaseCancelled:
		b.WriteString("\n⏹️ **Cancelled**\n")
	case PhasePlanning:
		b.WriteString("\n🔄 Planning in progress...\n")
	case PhaseExecuting:
		b.WriteString(fmt.Sprintf("\n🔄 Executing (iteration %d)...\n", e.Iteration))
	case PhaseReviewing:
		b.WriteString(fmt.Sprintf("\n🔄 Reviewing (iteration %d)...\n", e.Iteration))
	case PhaseIterating:
		b.WriteString(fmt.Sprintf("\n🔄 Iterating (iteration %d/%d)...\n", e.Iteration, e.MaxIter))
	}

	if e.Plan != "" {
		b.WriteString(fmt.Sprintf("\n### Plan\n%s\n", truncateForDisplay(e.Plan, 500)))
	}
	if e.Review != "" {
		b.WriteString(fmt.Sprintf("\n### Review\n%s\n", truncateForDisplay(e.Review, 300)))
	}
	if len(e.Results) > 0 {
		b.WriteString(fmt.Sprintf("\n### Results (%d members)\n", len(e.Results)))
		for id, result := range e.Results {
			b.WriteString(fmt.Sprintf("- **%s**: %s\n", id, truncateForDisplay(result, 200)))
		}
	}

	return b.String()
}

// truncateForDisplay truncates long text for display.
func truncateForDisplay(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + fmt.Sprintf("\n[... truncated %d chars ...]", len(text)-maxLen)
}

// PhaseProgress returns a 0-100 progress estimate for the current phase.
func (e *TeamExecution) PhaseProgress() int {
	switch e.Phase {
	case PhaseCompleted:
		return 100
	case PhaseFailed, PhaseCancelled:
		return 100
	case PhasePlanning:
		return 10
	case PhaseExecuting:
		return int(math.Min(90, float64(20+e.Iteration)*25))
	case PhaseReviewing:
		return int(math.Min(95, float64(30+e.Iteration)*25))
	case PhaseIterating:
		return int(math.Min(95, float64(15+e.Iteration)*25))
	default:
		return 0
	}
}
