package telegram

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveLoadTokenRoundtrip(t *testing.T) {
	dir := t.TempDir()

	_, err := LoadToken(dir)
	require.Error(t, err, "no token saved yet")

	SaveToken(dir, "123:ABC")
	got, err := LoadToken(dir)
	require.NoError(t, err)
	assert.Equal(t, "123:ABC", got)

	// Overwrite keeps only the last token.
	SaveToken(dir, "456:DEF")
	got, err = LoadToken(dir)
	require.NoError(t, err)
	assert.Equal(t, "456:DEF", got)
}

func TestSaveTokenSkipsEmpty(t *testing.T) {
	dir := t.TempDir()
	SaveToken(dir, "")
	SaveToken("", "123:ABC")
	_, err := os.Stat(filepath.Join(dir, tokensFileName))
	assert.True(t, os.IsNotExist(err))

	_, err = LoadToken("")
	assert.NoError(t, err, "empty dir must not error")
}

func TestSaveTokenFilePermissions(t *testing.T) {
	dir := t.TempDir()
	SaveToken(dir, "123:ABC")
	info, err := os.Stat(filepath.Join(dir, tokensFileName))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestMaskedToken(t *testing.T) {
	assert.Equal(t, "", MaskedToken(""))
	assert.Equal(t, "...7890", MaskedToken("1234567890:AAF-xyz7890"))
}

func TestTakeoverAllowed(t *testing.T) {
	dir := t.TempDir()
	alive := map[int]bool{100: true}
	isAlive := func(pid int) bool { return alive[pid] }

	assert.Equal(t, TakeoverFree, TakeoverAllowed(dir, "bot", "s1", isAlive))

	ClaimOwner(dir, Owner{Username: "bot", SessionID: "s1", PID: 100})
	assert.Equal(t, TakeoverSame, TakeoverAllowed(dir, "bot", "s1", isAlive))
	assert.Equal(t, TakeoverConfirm, TakeoverAllowed(dir, "bot", "s2", isAlive))

	// Dead process behaves as free so a stale lock never blocks.
	ClaimOwner(dir, Owner{Username: "bot", SessionID: "s1", PID: 999})
	assert.Equal(t, TakeoverFree, TakeoverAllowed(dir, "bot", "s2", isAlive))

	// Different bot is unaffected.
	assert.Equal(t, TakeoverFree, TakeoverAllowed(dir, "other", "s2", isAlive))

	// Release only drops the matching session.
	ClaimOwner(dir, Owner{Username: "bot", SessionID: "s1", PID: 100})
	ReleaseOwner(dir, "s2")
	_, ok := LoadOwner(dir)
	assert.True(t, ok, "other session must not release the claim")
	ReleaseOwner(dir, "s1")
	_, ok = LoadOwner(dir)
	assert.False(t, ok)
}

func TestDecodeOwnerRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"", "a\nb", "a\nb\n0", "a\nb\n-1", "a\nb\nzz"} {
		_, ok := decodeOwner(bad)
		assert.False(t, ok, "input %q must be rejected", bad)
	}
}

func TestProcessAlive(t *testing.T) {
	assert.True(t, ProcessAlive(os.Getpid()))
	assert.False(t, ProcessAlive(0))
	assert.False(t, ProcessAlive(-1))
}
