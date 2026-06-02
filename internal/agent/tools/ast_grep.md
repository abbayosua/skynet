# AST-grep Tools

Search and rewrite code using AST pattern matching (25 languages supported).

## ast_grep_search
Search code by AST structure. Uses meta-variables, NOT regex.

**Meta-variables:**
- `$VAR` - one AST node (identifier, expression, statement)
- `$$$` - zero or more nodes (argument lists, function bodies)

**Parameters:**
- `pattern` (required): AST pattern to search for (e.g., `"console.log($MSG)"`)
- `lang` (optional): Language (typescript, go, python, rust, etc.)
- `paths` (optional): Paths to search (defaults to working directory)
- `context` (optional): Context lines around matches

**Does NOT work with regex** - use `grep` for text search.

## ast_grep_replace
Rewrite code by AST pattern. Dry-run by default.

**Parameters:**
- `pattern` (required): AST pattern to match
- `rewrite` (required): Replacement template using $VAR from the pattern
- `lang` (optional): Language
- `paths` (optional): File or directory to run on
- `dry_run` (optional): Preview without applying (default: true)

**Examples:**
- Search: `{"pattern": "console.log($MSG)", "lang": "typescript"}`
- Replace: `{"pattern": "console.log($MSG)", "rewrite": "logger.info($MSG)", "dry_run": true}`