package tools

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"charm.land/fantasy"
	"github.com/code-yeongyu/skynet/internal/filepathext"
	"github.com/code-yeongyu/skynet/internal/permission"
)

//go:embed ast_grep.md
var astGrepDescription string

const (
	ASTGrepSearchToolName  = "ast_grep_search"
	ASTGrepReplaceToolName = "ast_grep_replace"
)

type ASTGrepSearchParams struct {
	Pattern  string   `json:"pattern" description:"AST pattern to search for (e.g. 'console.log($MSG)')"`
	Lang     string   `json:"lang,omitempty" description:"Language for the pattern (e.g. typescript, go, python, rust). Auto-detected from file extension if not specified."`
	Paths    []string `json:"paths,omitempty" description:"Paths to search in (defaults to working directory)"`
	Context  int      `json:"context,omitempty" description:"Number of context lines around matches (default: 0)"`
}

type ASTGrepReplaceParams struct {
	Pattern  string `json:"pattern" description:"AST pattern to match (e.g. 'console.log($MSG)')"`
	Rewrite  string `json:"rewrite" description:"Replacement pattern using $VAR from the pattern (e.g. 'logger.info($MSG)')"`
	Lang     string `json:"lang,omitempty" description:"Language for the pattern"`
	Paths    string `json:"paths,omitempty" description:"Directory or file path to run replacement on"`
	DryRun   bool   `json:"dry_run,omitempty" description:"Preview changes without applying (default: true)"`
}

// findASTGrepBinary locates the ast-grep executable.
func findASTGrepBinary() (string, error) {
	// Check common locations.
	candidates := []string{
		"ast-grep",
		"sg",
		filepath.Join("node_modules", "@ast-grep", "cli", "ast-grep.exe"),
		filepath.Join("node_modules", "@ast-grep", "cli", "sg.exe"),
	}

	// Try local project first.
	for _, candidate := range candidates {
		if filepath.IsLocal(candidate) {
			if fullPath, err := filepath.Abs(candidate); err == nil {
				if _, err := exec.LookPath(fullPath); err == nil {
					return fullPath, nil
				}
			}
		}
	}

	// Try PATH.
	for _, candidate := range candidates {
		if !filepath.IsLocal(candidate) {
			if path, err := exec.LookPath(candidate); err == nil {
				return path, nil
			}
		}
	}

	return "", fmt.Errorf("ast-grep not found. Install it via: npm install @ast-grep/cli")
}

func NewASTGrepSearchTool(workingDir string) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		ASTGrepSearchToolName,
		"Search code by AST pattern (25 languages). Uses meta-variables like $VAR (one node) and $$$ (many nodes). NOT regex: no alternation, no wildcards. For text search use grep instead.",
		func(ctx context.Context, params ASTGrepSearchParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			binary, err := findASTGrepBinary()
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}

			args := []string{"run"}

			// Working directory.
			searchPath := workingDir
			if len(params.Paths) > 0 {
				searchPath = filepathext.SmartJoin(workingDir, params.Paths[0])
			}

			if params.Pattern == "" {
				return fantasy.NewTextErrorResponse("pattern is required"), nil
			}
			args = append(args, "--pattern", params.Pattern)

			if params.Lang != "" {
				args = append(args, "--lang", params.Lang)
			}

			if params.Context > 0 {
				args = append(args, "--context", fmt.Sprintf("%d", params.Context))
			}

			// Add path at the end.
			args = append(args, searchPath)

			var stderr bytes.Buffer
			cmd := exec.CommandContext(ctx, binary, args...)
			cmd.Dir = workingDir
			cmd.Stderr = &stderr

			output, err := cmd.Output()
			if err != nil {
				if len(output) == 0 {
					return fantasy.NewTextErrorResponse(fmt.Sprintf("ast-grep error: %s", strings.TrimSpace(stderr.String()))), nil
				}
			}

			result := string(output)
			if strings.TrimSpace(result) == "" {
				return fantasy.NewTextResponse("No matches found."), nil
			}

			return fantasy.NewTextResponse(fmt.Sprintf("<ast-grep-results>\n%s\n</ast-grep-results>", result)), nil
		})
}

func NewASTGrepReplaceTool(workingDir string, permissions permission.Service) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		ASTGrepReplaceToolName,
		"Rewrite code by AST pattern (25 languages). Dry-run by default. Uses meta-variables: $VAR (one node) and $$$ (many nodes). Pattern matched then replaced with rewrite template.",
		func(ctx context.Context, params ASTGrepReplaceParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			binary, err := findASTGrepBinary()
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}

			if params.Pattern == "" {
				return fantasy.NewTextErrorResponse("pattern is required"), nil
			}
			if params.Rewrite == "" {
				return fantasy.NewTextErrorResponse("rewrite is required"), nil
			}

			// When not a dry-run, require permission approval.
			if params.DryRun == false {
				sessionID := GetSessionFromContext(ctx)
				if sessionID == "" {
					return fantasy.ToolResponse{}, fmt.Errorf("session ID is required for file modifications")
				}

				desc := fmt.Sprintf("AST-grep rewrite in %s: pattern=%q rewrite=%q", params.Paths, params.Pattern, params.Rewrite)
				searchPath := workingDir
				if params.Paths != "" {
					searchPath = filepathext.SmartJoin(workingDir, params.Paths)
				}
				granted, permErr := permissions.Request(ctx,
					permission.CreatePermissionRequest{
						SessionID:   sessionID,
						Path:        searchPath,
						ToolCallID:  call.ID,
						ToolName:    ASTGrepReplaceToolName,
						Action:      "write",
						Description: desc,
						Params:      params,
					},
				)
				if permErr != nil {
					return fantasy.ToolResponse{}, permErr
				}
				if !granted {
					return NewPermissionDeniedResponse(), nil
				}
			}

			args := []string{"run"}

			searchPath := workingDir
			if params.Paths != "" {
				searchPath = filepathext.SmartJoin(workingDir, params.Paths)
			}

			// Default to dry-run for safety.
			dryRun := true
			if !params.DryRun {
				dryRun = false
			}

			args = append(args, "--pattern", params.Pattern)
			args = append(args, "--rewrite", params.Rewrite)

			if params.Lang != "" {
				args = append(args, "--lang", params.Lang)
			}

			if !dryRun {
				// --update-all actually applies the rewrite (without it, ast-grep shows diff only)
				args = append(args, "--update-all")
			}

			args = append(args, searchPath)

			var stderr bytes.Buffer
			cmd := exec.CommandContext(ctx, binary, args...)
			cmd.Dir = workingDir
			cmd.Stderr = &stderr

			output, err := cmd.Output()
			if err != nil {
				if len(output) == 0 {
					return fantasy.NewTextErrorResponse(fmt.Sprintf("ast-grep error: %s", strings.TrimSpace(stderr.String()))), nil
				}
			}

			result := string(output)
			if strings.TrimSpace(result) == "" && dryRun {
				return fantasy.NewTextResponse("No changes to apply."), nil
			}

			if dryRun {
				return fantasy.NewTextResponse(fmt.Sprintf(
					"<ast-grep-preview>\n%s\n</ast-grep-preview>\n\nThis was a dry run. Call again with dry_run=false to apply changes.",
					result,
				)), nil
			}

			return fantasy.NewTextResponse(fmt.Sprintf("<ast-grep-result>\n%s\n</ast-grep-result>", result)), nil
		})
}
