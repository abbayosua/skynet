Create and manage work plans in `.omo/plans/`.

Use this tool BEFORE starting implementation on any non-trivial task. A good plan saves time and prevents missed requirements.

**Actions:**
- `create`: Create a new structured plan. Provide a `task` description of what needs to be built. Optionally include `context` (codebase details, constraints) and `acceptance` (specific success criteria). A plan file will be created at `.omo/plans/{name}.md`.
- `list`: List all existing plans in `.omo/plans/`.

**How to use:**
1. First, understand the user's request thoroughly. Ask clarifying questions if needed.
2. Call `planner` with action="create" and a clear task description.
3. After the plan is created, use `view` to read it, then fill in the details using `edit` or `hashline_edit`.
4. When ready to start work, tell the user the plan is ready.

**Example:**
```
{"action": "create", "task": "Add user authentication with JWT tokens", "context": "Go API server using Chi router, PostgreSQL database", "acceptance": "Register, login, token refresh, protected routes"}
```