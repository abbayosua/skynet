package prompt

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abbayosua/skynet/internal/config"
)

// TestIsDeepSeekCachePromptAcceptsProviderIDs guards the regression where the
// check was fed fantasy's provider name ("openai-compat") instead of the
// configured provider id, so the stable-prefix path never activated for
// opencode and the prefix cache missed on every turn.
func TestIsDeepSeekCachePromptAcceptsProviderIDs(t *testing.T) {
	t.Parallel()

	for _, provider := range []string{
		"opencode-go",
		"opencode-zen",
		"opencode-zen-bangdjarot",
		"deepseek",
		"b-ai-deepseek",
	} {
		assert.True(t, isDeepSeekCachePrompt(provider, "muse-spark-1.3-contributor"),
			"provider %q should use the stable-prefix path", provider)
	}

	// The model name still carries providers that host these models remotely.
	assert.True(t, isDeepSeekCachePrompt("commandcode", "deepseek/deepseek-v4-pro"))
	assert.True(t, isDeepSeekCachePrompt("proxyrouter", "mimo-v2.5-free"))

	// Unrelated providers keep the full, volatile snapshot.
	assert.False(t, isDeepSeekCachePrompt("anthropic", "claude-sonnet-5"))
	assert.False(t, isDeepSeekCachePrompt("openai", "gpt-5.6-sol"))
}

// TestPromptDataStabilizesCachePrefix verifies that a cache-model provider gets
// a prefix that survives file edits inside one session. The git snapshot is the
// largest volatile block in the system prompt: when it changes, upstream's
// prefix cache is invalidated from that byte onward.
func TestPromptDataStabilizesCachePrefix(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("a\n"), 0o644))
	runGit(t, dir, "add", "tracked.txt")
	runGit(t, dir, "commit", "-m", "init")

	store, err := config.Init(dir, t.TempDir(), false)
	require.NoError(t, err)

	p, err := NewPrompt("coder", "irrelevant", WithWorkingDir(dir))
	require.NoError(t, err)

	before, err := p.promptData(t.Context(), "opencode-go", "muse-spark-1.3-contributor", store)
	require.NoError(t, err)

	assert.Equal(t, "1/1/2006", before.Date, "the date must be pinned so it cannot invalidate the prefix")
	assert.Contains(t, before.GitStatus, "Current branch")
	assert.NotContains(t, before.GitStatus, "tracked.txt",
		"cache models must not embed the volatile status listing")
	assert.NotContains(t, before.GitStatus, "Recent commits")

	// A new file changes `git status` output but must not change the prefix.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("b\n"), 0o644))

	after, err := p.promptData(t.Context(), "opencode-go", "muse-spark-1.3-contributor", store)
	require.NoError(t, err)
	assert.Equal(t, before.GitStatus, after.GitStatus,
		"the git snapshot must stay byte-identical across turns in a session")
	assert.Equal(t, before.Date, after.Date)

	// Non-cache providers still get the full snapshot.
	plain, err := p.promptData(t.Context(), "anthropic", "claude-sonnet-5", store)
	require.NoError(t, err)
	assert.Contains(t, plain.GitStatus, "untracked.txt")
	assert.NotEqual(t, "1/1/2006", plain.Date)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}
