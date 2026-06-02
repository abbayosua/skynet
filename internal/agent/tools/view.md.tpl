Read a file by path with line numbers and content hashes (e.g. `    15#VKMB|func hello() {`); supports offset and line limit (default {{ .DefaultReadLimit }}, max {{ .MaxViewSizeKB }}KB returned file content section); renders images (PNG, JPEG, GIF, WebP); use ls for directories.

Each line is prefixed with `LINENUMBER#HASH|` where HASH is a 4-character content hash (65,536 possible values, very low collision probability). Use the hashline_edit tool with the `line_id` parameter (e.g. "15#VKMB") for safer edits — the hash validates that the line content hasn't changed since you read it.
