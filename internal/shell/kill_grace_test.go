package shell

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBackgroundShellManager_Kill_GraceTimeout(t *testing.T) {
	t.Parallel()

	workingDir := t.TempDir()
	manager := newBackgroundShellManager()

	// Start a normal shell to verify Kill returns quickly when shell honors cancel.
	_, err := manager.Start(t.Context(), workingDir, nil, "sleep 60", "")
	require.NoError(t, err)

	var id string
	for shell := range manager.shells.Seq() {
		id = shell.ID
		break
	}
	require.NotEmpty(t, id)

	start := time.Now()
	err = manager.Kill(id)
	elapsed := time.Since(start)

	require.NoError(t, err)
	// Must return promptly, not after 60s.
	require.Less(t, elapsed, 4*time.Second, "Kill should not block forever")
}

func TestBackgroundShellManager_Kill_AbandonsHungJob(t *testing.T) {
	t.Parallel()

	manager := newBackgroundShellManager()

	// Manually inject a hung shell whose done never closes to simulate
	// a job that ignores SIGTERM and holds pipe FDs open.
	_, cancel := context.WithCancel(context.Background())
	hung := &BackgroundShell{
		ID:        "HUNG",
		Command:   "hung",
		done:      make(chan struct{}), // never closed
		cancel:    cancel,
		stdout:    &syncBuffer{},
		stderr:    &syncBuffer{},
		WorkingDir: "/tmp",
	}
	manager.shells.Set(hung.ID, hung)

	start := time.Now()
	err := manager.Kill(hung.ID)
	elapsed := time.Since(start)

	require.NoError(t, err)
	// Must abandon after ~2s grace, not hang forever.
	require.Less(t, elapsed, 4*time.Second)
	require.GreaterOrEqual(t, elapsed, 1900*time.Millisecond)
	// Shell should be removed from manager even though done never closed.
	_, ok := manager.Get(hung.ID)
	require.False(t, ok)
}
