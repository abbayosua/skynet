package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoderTemplateHasTodoEnforcer(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("templates", "coder.md.tpl"))
	if err != nil {
		t.Fatal("failed to read coder.md.tpl:", err)
	}
	content := string(data)

	checks := []string{
		"{{if .TaskPlannerEnabled}}",
		"<todo_enforcer>",
		"Plan first",
		"Track each step",
		"Verify before DONE",
		"No shortcuts",
		"Re-check on resume",
		"<promise>DONE</promise>",
		"{{end}}",
	}
	for _, c := range checks {
		if !strings.Contains(content, c) {
			t.Errorf("coder.md.tpl missing: %q", c)
		}
	}
}

func TestRalphLoopPromptHasTodoCheck(t *testing.T) {
	// Read agent.go to verify the Ralph Loop continuation prompt
	// references checking todos before declaring DONE.
	data, err := os.ReadFile("agent.go")
	if err != nil {
		t.Fatal("failed to read agent.go:", err)
	}
	content := string(data)

	// The Ralph Loop continuation should instruct the agent to check todos.
	required := []string{
		"Check your todo list",
		"any todo items remain pending",
		"<promise>DONE</promise>",
	}
	for _, r := range required {
		if !strings.Contains(content, r) {
			t.Errorf("agent.go Ralph Loop prompt missing: %q", r)
		}
	}
}
