# Background Agent Tools

SkyNet provides three tools for running agents in the background while you continue working:

## spawn_agent
Spawn a background agent to perform a task asynchronously. You can continue working while it runs.

**Parameters:**
- `prompt` (required): The task for the agent to perform.
- `description` (optional): A short description for reference.
- `timeout_seconds` (optional): Maximum execution time in seconds (default: 600, max: 3600).

**Returns:** An agent ID like `bg_123456789_1` that you can use with `agent_status` and `collect_agent`.

## agent_status
Check the status of a previously spawned background agent.

**Parameters:**
- `agent_id` (required): The agent ID returned by `spawn_agent`.

**Returns:** Current status (queued/running/completed/error/cancelled) with timing info.

## collect_agent
Retrieve the final result of a completed background agent.

**Parameters:**
- `agent_id` (required): The agent ID returned by `spawn_agent`.

**Returns:** The agent's output once it completes. If still running, you'll be told to check again later.

## Usage Pattern

1. `spawn_agent` with your task → get agent_id
2. Continue working on other things
3. `agent_status` to check progress
4. `collect_agent` to get results when done

## How It Works

Background agents run in a separate goroutine with its own LLM session. Each agent:
- Gets an independent context with configurable timeout (default: 10 minutes)
- Runs concurrently with up to 5 agents at a time by default
- Uses the task agent configuration (read-only tools for safe background analysis)
- Is automatically cancelled if it exceeds the timeout

Results are stored in memory and can be collected with `collect_agent` once the status shows as completed.
