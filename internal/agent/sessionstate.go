package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const maxSnapshotFunctions = 20

type functionInfo struct {
	File    string   `json:"file"`
	Purpose string   `json:"purpose"`
	Calls   []string `json:"calls,omitempty"`
	Deleted bool     `json:"deleted,omitempty"`
}

type sessionSnapshot struct {
	ModifiedFiles []string                `json:"modified_files,omitempty"`
	GitBranch     string                  `json:"git_branch,omitempty"`
	Functions     map[string]functionInfo `json:"functions,omitempty"`
	CredsRefs     []string                `json:"creds_ref,omitempty"`
}

func buildSessionSnapshot(workingDir string) *sessionSnapshot {
	snap := &sessionSnapshot{Functions: map[string]functionInfo{}}
	snap.GitBranch = gitBranch(workingDir)
	snap.ModifiedFiles = gitModifiedFiles(workingDir)
	if len(snap.ModifiedFiles) == 0 {
		return snap
	}
	for _, f := range snap.ModifiedFiles {
		if !strings.HasSuffix(f, ".go") {
			continue
		}
		abs := filepath.Join(workingDir, f)
		for name, fi := range parseGoFunctions(abs, f) {
			if len(snap.Functions) >= maxSnapshotFunctions {
				break
			}
			fi.Calls = findCallers(workingDir, name, f)
			snap.Functions[name] = fi
		}
	}
	snap.CredsRefs = detectCredsRefs(workingDir)
	return snap
}

func (s *sessionSnapshot) render() string {
	if s == nil || (len(s.Functions) == 0 && len(s.ModifiedFiles) == 0) {
		return ""
	}
	data, err := json.Marshal(s)
	if err != nil {
		slog.Warn("Failed to marshal session snapshot", "error", err)
		return ""
	}
	return "<session_state>\n" + string(data) + "\n</session_state>"
}

func gitBranch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func gitModifiedFiles(dir string) []string {
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain").Output()
	if err != nil {
		return nil
	}
	var files []string
	for line := range strings.Lines(string(out)) {
		line = strings.TrimSpace(line)
		if len(line) < 4 {
			continue
		}
		files = append(files, strings.TrimSpace(line[2:]))
	}
	return files
}

func parseGoFunctions(absPath, relPath string) map[string]functionInfo {
	result := map[string]functionInfo{}
	src, err := os.ReadFile(absPath)
	if err != nil {
		return result
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, absPath, src, parser.ParseComments)
	if err != nil {
		return result
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil {
			continue
		}
		purpose := ""
		if fn.Doc != nil {
			purpose = strings.TrimSpace(fn.Doc.Text())
			if idx := strings.Index(purpose, "\n"); idx >= 0 {
				purpose = purpose[:idx]
			}
		}
		if purpose == "" {
			purpose = firstCodeLine(fn, src)
		}
		result[fn.Name.Name] = functionInfo{File: relPath, Purpose: purpose}
	}
	return result
}

func firstCodeLine(fn *ast.FuncDecl, src []byte) string {
	if fn.Body == nil || len(fn.Body.List) == 0 {
		return ""
	}
	start := fn.Body.List[0].Pos()
	end := fn.Body.List[0].End()
	line := strings.TrimSpace(string(src[start-1 : end-1]))
	if len(line) > 80 {
		line = line[:80]
	}
	return line
}

func findCallers(workingDir, funcName, excludeFile string) []string {
	var callers []string
	seen := map[string]bool{}
	filepath.WalkDir(workingDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && (d.Name() == ".git" || d.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(workingDir, path)
		if rel == excludeFile {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if bytes.Contains(data, []byte(funcName+"(")) {
			if fns := parseGoFunctions(path, rel); len(fns) > 0 {
				for caller := range fns {
					if !seen[caller] {
						seen[caller] = true
						callers = append(callers, caller)
						if len(callers) >= 5 {
							return filepath.SkipAll
						}
					}
				}
			}
		}
		return nil
	})
	return callers
}

func detectCredsRefs(workingDir string) []string {
	var refs []string
	if _, err := os.Stat(filepath.Join(workingDir, ".env")); err == nil {
		refs = append(refs, ".env")
	}
	return refs
}

func appendSnapshotToSummaryPrompt(base string) string {
	workingDir, err := os.Getwd()
	if err != nil {
		return base
	}
	snap := buildSessionSnapshot(workingDir)
	rendered := snap.render()
	if rendered == "" {
		return base
	}
	return base + "\n\n" + rendered + "\n\nUse the <session_state> above as ground truth for what changed this session. " +
		"Do not contradict it in your summary." + fmt.Sprintf(" (%d functions tracked)", len(snap.Functions))
}
