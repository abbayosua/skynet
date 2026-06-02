Coordinate multi-agent teams to work on complex tasks together.

**Actions:**
- `create`: Create a new team. Provide `team_name` and `task` describing the team's objective.
- `add_member`: Add a member to the team. Provide `team_name`, `member_id` (e.g. "backend", "frontend", "tester"), and optionally `member_task`.
- `run`: Execute the full team workflow. The team will:
  1. **Plan**: Analyze the task and create an execution plan
  2. **Execute**: All members work in parallel on their assigned tasks
  3. **Review**: Review the combined results for quality and correctness
  4. **Iterate**: If review fails, members fix issues and the cycle repeats
- `list`: List all active teams and their current status.
- `status`: Show detailed status of a team including execution phase, plan, review feedback, and member states.
- `cancel`: Cancel a running team execution.

**Usage Pattern:**

1. Create a team:
   `{"action": "create", "team_name": "auth-feature", "task": "Implement JWT authentication"}`
2. Add members with specific roles:
   - `{"action": "add_member", "team_name": "auth-feature", "member_id": "backend", "member_task": "Implement auth endpoints and middleware"}`
   - `{"action": "add_member", "team_name": "auth-feature", "member_id": "tests", "member_task": "Write integration tests for auth"}`
3. Run the team workflow:
   `{"action": "run", "team_name": "auth-feature"}`
4. Check progress:
   `{"action": "status", "team_name": "auth-feature"}`

**How It Works:**

The team orchestrator runs a multi-phase workflow:
1. **Planning phase**: A planner agent analyzes the task and creates a structured execution plan
2. **Execution phase**: All team members receive their assigned sub-tasks and work in parallel (up to 5 concurrent agents)
3. **Review phase**: A reviewer agent evaluates the combined output for quality
4. **Iterate**: If the review identifies issues, execution repeats with the feedback incorporated (up to 3 iterations)

Team state is saved to `.omo/teams/` for persistence across sessions.
