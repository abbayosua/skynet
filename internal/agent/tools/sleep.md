Sleep for a specified duration without blocking via bash.

<usage>
- Use instead of `bash` with `sleep 30` to avoid spawning a shell
- Provide either `duration` (Go duration string like "30s", "1.5s", "2m") or `seconds` (number)
- Example: `{"duration": "30s"}` or `{"seconds": 30}` or `{"duration": "500ms"}`
- Max duration is 300 seconds (5 minutes); larger values are rejected
</usage>

<features>
- Context-aware: respects cancellation
- No permission prompt required
- Lightweight: uses Go time.After, no shell process
</features>

<tips>
- Prefer this over `bash` `sleep` for simple delays
- For waiting on background jobs, use `job_output` with `wait: true` instead
- Duration strings support units: ns, us, ms, s, m, h (e.g. "1m30s")
</tips>
