package tools

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"charm.land/fantasy"
	"github.com/abbayosua/skynet/internal/diff"
	"github.com/abbayosua/skynet/internal/filepathext"
	"github.com/abbayosua/skynet/internal/filetracker"
	"github.com/abbayosua/skynet/internal/fsext"
	"github.com/abbayosua/skynet/internal/hashline"
	"github.com/abbayosua/skynet/internal/history"
	"github.com/abbayosua/skynet/internal/lsp"
	"github.com/abbayosua/skynet/internal/permission"
)

//go:embed hashline_edit.md
var hashlineEditDescription string

const HashlineEditToolName = "hashline_edit"

type HashlineEditParams struct {
	FilePath   string `json:"file_path" description:"REQUIRED: The absolute path to the file to modify (required, do not omit)"`
	LineID     string `json:"line_id" description:"REQUIRED: The LINE#ID of the line to replace (e.g. \"15#VKMB\"), as shown by the View tool output (required, do not omit)"`
	NewContent string `json:"new_content" description:"REQUIRED: The new line content to replace the old line with (required, do not omit)"`
}

func NewHashlineEditTool(
	lspManager *lsp.Manager,
	permissions permission.Service,
	files history.Service,
	filetracker filetracker.Service,
	workingDir string,
) fantasy.AgentTool {
	return fantasy.NewAgentTool(
		HashlineEditToolName,
		hashlineEditDescription,
		func(ctx context.Context, params HashlineEditParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if params.FilePath == "" {
				return fantasy.NewTextErrorResponse("file_path is required"), nil
			}

			ReportActivity(ctx, "Editing: "+params.FilePath)

			if params.LineID == "" {
				return fantasy.NewTextErrorResponse("line_id is required (use the LINE#ID format from View output, e.g. \"15#VK\")"), nil
			}

			params.FilePath = filepathext.SmartJoin(workingDir, params.FilePath)

			// Parse the LINE#ID reference.
			lid := hashline.ParseLineID(params.LineID)
			if lid == nil {
				return fantasy.NewTextErrorResponse(fmt.Sprintf(
					"invalid line_id format: %q. Expected format is LINE#HASH (e.g. \"15#VKMB\") as shown in View output",
					params.LineID,
				)), nil
			}

			// Check file exists and is not a directory.
			fileInfo, err := os.Stat(params.FilePath)
			if err != nil {
				if os.IsNotExist(err) {
					return fantasy.NewTextErrorResponse(fmt.Sprintf("file not found: %s", params.FilePath)), nil
				}
				return fantasy.ToolResponse{}, fmt.Errorf("failed to access file: %w", err)
			}
			if fileInfo.IsDir() {
				return fantasy.NewTextErrorResponse(fmt.Sprintf("path is a directory, not a file: %s", params.FilePath)), nil
			}

			sessionID := GetSessionFromContext(ctx)
			if sessionID == "" {
				return fantasy.ToolResponse{}, fmt.Errorf("session ID is required")
			}

			// Verify the file was recently read.
			lastRead := filetracker.LastReadTime(ctx, sessionID, params.FilePath)
			if lastRead.IsZero() {
				return fantasy.NewTextErrorResponse("you must read the file before editing it. Use the View tool first"), nil
			}

			modTime := fileInfo.ModTime().Truncate(time.Second)
			if modTime.After(lastRead) {
				return fantasy.NewTextErrorResponse(
					fmt.Sprintf("file %s has been modified since it was last read (mod time: %s, last read: %s)",
						params.FilePath, modTime.Format(time.RFC3339), lastRead.Format(time.RFC3339),
					)), nil
			}

			// Read current file content.
			content, err := os.ReadFile(params.FilePath)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("failed to read file: %w", err)
			}

			oldContent, isCrlf := fsext.ToUnixLineEndings(string(content))
			lines := strings.Split(oldContent, "\n")

			// Validate line number (1-based).
			if lid.LineNumber < 1 || lid.LineNumber > len(lines) {
				return fantasy.NewTextErrorResponse(fmt.Sprintf(
					"line number %d is out of range. File has %d lines (1-%d)",
					lid.LineNumber, len(lines), len(lines),
				)), nil
			}

			// Get the current line content.
			oldLineContent := lines[lid.LineNumber-1]

			// Verify the hash matches the current content.
			if !hashline.VerifyLine(oldLineContent, lid.Hash) {
				return fantasy.NewTextErrorResponse(fmt.Sprintf(
					"content hash mismatch for line %d: expected hash %q but content has changed since you viewed the file. "+
						"The file has been modified by another source. Use the View tool to re-read the file and get updated hashes.",
					lid.LineNumber, lid.Hash,
				)), nil
			}

			// Replace the line.
			lines[lid.LineNumber-1] = params.NewContent
			newContent := strings.Join(lines, "\n")
			if isCrlf {
				newContent, _ = fsext.ToWindowsLineEndings(newContent)
			}

			if oldContent == newContent {
				return fantasy.NewTextErrorResponse("new content is the same as old content. No changes made."), nil
			}

			// Generate diff metadata.
			_, additions, removals := diff.GenerateDiff(
				oldContent,
				strings.Join(lines, "\n"),
				strings.TrimPrefix(params.FilePath, workingDir),
			)

			// Request permission.
			p, err := permissions.Request(ctx,
				permission.CreatePermissionRequest{
					SessionID:  sessionID,
					Path:       fsext.PathOrPrefix(params.FilePath, workingDir),
					ToolCallID: call.ID,
					ToolName:   HashlineEditToolName,
					Action:     "write",
					Description: fmt.Sprintf("Replace line %d in file %s",
						lid.LineNumber, params.FilePath),
					Params: EditPermissionsParams{
						FilePath:   params.FilePath,
						OldContent: oldContent,
						NewContent: strings.Join(lines, "\n"),
					},
				},
			)
			if err != nil {
				return fantasy.ToolResponse{}, err
			}
			if !p {
				return NewPermissionDeniedResponse(), nil
			}

			// Write the file.
			finalContent := strings.Join(lines, "\n")
			if isCrlf {
				finalContent, _ = fsext.ToWindowsLineEndings(finalContent)
			}
			err = os.WriteFile(params.FilePath, []byte(finalContent), 0o644)
			if err != nil {
				return fantasy.ToolResponse{}, fmt.Errorf("failed to write file: %w", err)
			}

			// Update history.
			file, err := files.GetByPathAndSession(ctx, params.FilePath, sessionID)
			if err != nil {
				_, err = files.Create(ctx, sessionID, params.FilePath, oldContent)
				if err != nil {
					return fantasy.ToolResponse{}, fmt.Errorf("error creating file history: %w", err)
				}
			}
			if file.Content != oldContent {
				_, err = files.CreateVersion(ctx, sessionID, params.FilePath, oldContent)
				if err != nil {
					slog.Debug("Error creating file history version", "error", err)
				}
			}
			_, err = files.CreateVersion(ctx, sessionID, params.FilePath, finalContent)
			if err != nil {
				slog.Error("Error creating file history version", "error", err)
			}

			filetracker.RecordRead(ctx, sessionID, params.FilePath)
			notifyLSPs(ctx, lspManager, params.FilePath)

			result := fmt.Sprintf("Line %d replaced in file: %s\n```\n%s\n```",
				lid.LineNumber, params.FilePath, params.NewContent)
			result += getDiagnostics(params.FilePath, lspManager)

			return fantasy.WithResponseMetadata(
				fantasy.NewTextResponse(result),
				EditResponseMetadata{
					OldContent: oldContent,
					NewContent: finalContent,
					Additions:  additions,
					Removals:   removals,
				},
			), nil
		})
}
