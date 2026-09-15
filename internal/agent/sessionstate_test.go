package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildSessionSnapshot_EmptyRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	snap := buildSessionSnapshot(dir)
	require.NotNil(t, snap)
	require.Empty(t, snap.ModifiedFiles)
	require.Empty(t, snap.Functions)
}

func TestBuildSessionSnapshot_GoFunctions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := "package foo\n\n// DoThing does a thing.\nfunc DoThing() string {\n\treturn \"x\"\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "foo.go"), []byte(src), 0o644))
	fns := parseGoFunctions(filepath.Join(dir, "foo.go"), "foo.go")
	require.Contains(t, fns, "DoThing")
	require.Equal(t, "foo.go", fns["DoThing"].File)
	require.Contains(t, fns["DoThing"].Purpose, "does a thing")
}

func TestBuildSessionSnapshot_PurposeFallback(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := "package foo\n\nfunc NoDoc() {\n\tfmt.Println(1)\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte(src), 0o644))
	fns := parseGoFunctions(filepath.Join(dir, "a.go"), "a.go")
	require.Contains(t, fns, "NoDoc")
	require.NotEmpty(t, fns["NoDoc"].Purpose)
}

func TestSessionSnapshot_RenderEmpty(t *testing.T) {
	t.Parallel()
	snap := &sessionSnapshot{Functions: map[string]functionInfo{}}
	require.Equal(t, "", snap.render())
}

func TestSessionSnapshot_RenderNonEmpty(t *testing.T) {
	t.Parallel()
	snap := &sessionSnapshot{
		ModifiedFiles: []string{"a.go"},
		GitBranch:     "main",
		Functions: map[string]functionInfo{
			"DoThing": {File: "a.go", Purpose: "does a thing", Calls: []string{"Main"}},
		},
	}
	rendered := snap.render()
	require.Contains(t, rendered, "<session_state>")
	require.Contains(t, rendered, "DoThing")
	require.Contains(t, rendered, "does a thing")
}

func TestAppendSnapshotToSummaryPrompt_NoChanges(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	defer func() { _ = os.Chdir(cwd) }()
	out := appendSnapshotToSummaryPrompt("base prompt")
	require.Equal(t, "base prompt", out)
}
