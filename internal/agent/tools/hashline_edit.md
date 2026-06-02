Edit a file by replacing a single line, identified by its LINE#ID hash from the View tool output.

Intended as a SAFER alternative to `edit`: instead of reproducing the exact old string, reference the `LINE#ID` shown in View output (e.g. `15#VKMB`). The tool verifies that the line content hasn't changed since you read it, then replaces the line.

Parameters:
- `file_path` (required): Absolute path to the file to edit.
- `line_id` (required): The LINE#ID tag from View output, e.g. `"15#VKMB"`. The #HASH portion is a 4-character content hash (65,536 possible values, very low collision probability) that validates the line hasn't been modified since you read it.
- `new_content` (required): The new line content to replace the old line.

Examples:
- Replace line 15: `{"file_path": "/app/main.go", "line_id": "15#VKMB", "new_content": "func hello() {"}`
- Note: the `line_id` must match exactly what was shown in View output, including the hash.

For multi-line edits, use the `edit` tool instead.