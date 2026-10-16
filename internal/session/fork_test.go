package session

import (
	"context"
	"testing"

	"github.com/abbayosua/skynet/internal/db"
	"github.com/stretchr/testify/require"
)

func newForkTestService(t *testing.T) (Service, *db.Queries) {
	t.Helper()
	conn, err := db.Connect(t.Context(), t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	q := db.New(conn)
	return NewService(q, conn), q
}

func TestForkCopiesHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, q := newForkTestService(t)

	src, err := svc.Create(ctx, "original")
	require.NoError(t, err)

	_, err = q.CreateMessage(ctx, db.CreateMessageParams{
		ID:        "msg-1",
		SessionID: src.ID,
		Role:      "user",
		Parts:     `[{"type":"text","data":{"text":"hello"}}]`,
	})
	require.NoError(t, err)
	_, err = q.CreateMessage(ctx, db.CreateMessageParams{
		ID:        "msg-2",
		SessionID: src.ID,
		Role:      "assistant",
		Parts:     `[{"type":"text","data":{"text":"hi"}}]`,
	})
	require.NoError(t, err)
	_, err = q.CreateFile(ctx, db.CreateFileParams{
		ID:        "file-1",
		SessionID: src.ID,
		Path:      "notes.md",
		Content:   "content",
		Version:   1,
	})
	require.NoError(t, err)
	require.NoError(t, q.RecordFileRead(ctx, db.RecordFileReadParams{
		SessionID: src.ID,
		Path:      "notes.md",
	}))

	forked, err := svc.Fork(ctx, src.ID)
	require.NoError(t, err)
	require.NotEqual(t, src.ID, forked.ID)
	require.Empty(t, forked.ParentSessionID)
	require.Equal(t, src.Title+" (fork)", forked.Title)

	srcMsgs, err := q.ListMessagesBySession(ctx, src.ID)
	require.NoError(t, err)
	forkMsgs, err := q.ListMessagesBySession(ctx, forked.ID)
	require.NoError(t, err)
	require.Len(t, srcMsgs, 2)
	require.Len(t, forkMsgs, 2)
	require.Equal(t, srcMsgs[0].Role, forkMsgs[0].Role)
	require.Equal(t, srcMsgs[0].Parts, forkMsgs[0].Parts)
	require.Equal(t, srcMsgs[1].Role, forkMsgs[1].Role)
	require.NotEqual(t, srcMsgs[0].ID, forkMsgs[0].ID)

	forkFiles, err := q.ListFilesBySession(ctx, forked.ID)
	require.NoError(t, err)
	require.Len(t, forkFiles, 1)
	require.Equal(t, "notes.md", forkFiles[0].Path)
	require.Equal(t, "content", forkFiles[0].Content)

	forkReads, err := q.ListSessionReadFiles(ctx, forked.ID)
	require.NoError(t, err)
	require.Len(t, forkReads, 1)

	updated, err := svc.Get(ctx, forked.ID)
	require.NoError(t, err)
	require.Equal(t, int64(2), updated.MessageCount)
}

func TestForkIsIndependent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, q := newForkTestService(t)

	src, err := svc.Create(ctx, "original")
	require.NoError(t, err)
	_, err = q.CreateMessage(ctx, db.CreateMessageParams{
		ID:        "msg-1",
		SessionID: src.ID,
		Role:      "user",
		Parts:     `[{"type":"text","data":{"text":"hello"}}]`,
	})
	require.NoError(t, err)

	forked, err := svc.Fork(ctx, src.ID)
	require.NoError(t, err)

	_, err = q.CreateMessage(ctx, db.CreateMessageParams{
		ID:        "msg-fork-only",
		SessionID: forked.ID,
		Role:      "user",
		Parts:     `[{"type":"text","data":{"text":"fork continues"}}]`,
	})
	require.NoError(t, err)

	srcMsgs, err := q.ListMessagesBySession(ctx, src.ID)
	require.NoError(t, err)
	require.Len(t, srcMsgs, 1)

	forkMsgs, err := q.ListMessagesBySession(ctx, forked.ID)
	require.NoError(t, err)
	require.Len(t, forkMsgs, 2)

	require.NoError(t, svc.Delete(ctx, forked.ID))
	_, err = svc.Get(ctx, src.ID)
	require.NoError(t, err)
}

func TestForkMissingSession(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newForkTestService(t)

	_, err := svc.Fork(ctx, "does-not-exist")
	require.Error(t, err)
}
