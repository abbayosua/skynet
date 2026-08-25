package agent

import (
	"context"
	"errors"
	"net"
	"testing"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
	"github.com/abbayosua/skynet/internal/config"
	"github.com/abbayosua/skynet/internal/csync"
	"github.com/abbayosua/skynet/internal/session"
	"github.com/stretchr/testify/require"
)

func TestShouldAutoRetry_NetworkErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"context canceled", contextCanceledError(), false},
		{"context deadline", contextDeadlineError(), false},
		{"endpoint unavailable", errors.New("endpoint is unavailable"), true},
		{"model unavailable", errors.New("Model is unavailable: test"), true},
		{"no such host", errors.New("dial tcp: lookup api.openai.com: no such host"), true},
		{"dial tcp", errors.New("dial tcp 1.2.3.4:443: connect: connection refused"), true},
		{"connection refused", errors.New("connection refused"), true},
		{"connection reset", errors.New("read tcp: connection reset by peer"), true},
		{"broken pipe", errors.New("write: broken pipe"), true},
		{"network unreachable", errors.New("dial tcp: network is unreachable"), true},
		{"no route to host", errors.New("dial tcp: no route to host"), true},
		{"i/o timeout", errors.New("read tcp: i/o timeout"), true},
		{"timeout generic", errors.New("Client.Timeout exceeded while awaiting headers"), true},
		{"tls handshake timeout", errors.New("net/http: TLS handshake timeout"), true},
		{"unexpected eof", errors.New("unexpected EOF"), true},
		{"eof", errors.New("EOF"), true},
		{"lookup", errors.New("lookup api.anthropic.com: no such host"), true},
		{"temporary failure", errors.New("lookup: temporary failure"), true},
		{"proxyconnect", errors.New("proxyconnect tcp: dial tcp: lookup proxy: no such host"), true},
		{"provider error retryable", &fantasy.ProviderError{Message: "Internal Server Error", StatusCode: 500}, true},
		{"provider error endpoint", &fantasy.ProviderError{Message: "endpoint is unavailable", StatusCode: 400}, true},
		{"provider error no such host", &fantasy.ProviderError{Message: "no such host"}, true},
		{"generic non retry", errors.New("invalid API key"), false},
		{"net.Error timeout", &netErrorMock{timeout: true}, true},
		{"retry error wrapping", &fantasy.RetryError{Errors: []error{errors.New("no such host")}}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := shouldAutoRetry(tc.err)
			require.Equal(t, tc.want, got, "shouldAutoRetry(%v)", tc.err)
		})
	}
}

func contextCanceledError() error {
	return context.Canceled
}

func contextDeadlineError() error {
	return context.DeadlineExceeded
}

type netErrorMock struct {
	timeout bool
}

func (e *netErrorMock) Error() string   { return "mock net error" }
func (e *netErrorMock) Timeout() bool   { return e.timeout }
func (e *netErrorMock) Temporary() bool { return true }

// Ensure netErrorMock implements net.Error
var _ net.Error = (*netErrorMock)(nil)

func TestIsTransientNetworkMessage(t *testing.T) {
	t.Parallel()
	require.True(t, isTransientNetworkMessage("dial tcp: lookup api.openai.com: no such host"))
	require.True(t, isTransientNetworkMessage("connection reset by peer"))
	require.False(t, isTransientNetworkMessage("invalid api key"))
	require.True(t, isTransientNetworkMessage("tls handshake timeout"))
}

func TestUpdateSessionUsage_GuardZero(t *testing.T) {
	t.Parallel()
	// Setup minimal session and model
	sess := session.Session{PromptTokens: 100, CompletionTokens: 200, Cost: 1.5}
	model := Model{
		CatwalkCfg: catwalk.Model{ID: "test-model"},
		ModelCfg:   config.SelectedModel{Model: "test", Provider: "test"},
	}
	a := &sessionAgent{
		largeModel: csync.NewValue(model),
		smallModel: csync.NewValue(model),
	}
	// Zero usage should not overwrite existing tokens (prevents 0% after network error)
	a.updateSessionUsage(model, &sess, fantasy.Usage{}, nil)
	require.Equal(t, int64(100), sess.PromptTokens, "PromptTokens should remain")
	require.Equal(t, int64(200), sess.CompletionTokens, "CompletionTokens should remain")
	require.Equal(t, 1.5, sess.Cost, "Cost should remain")

	// Non-zero usage should overwrite
	a.updateSessionUsage(model, &sess, fantasy.Usage{InputTokens: 10, OutputTokens: 20, CacheReadTokens: 5}, nil)
	require.Equal(t, int64(15), sess.PromptTokens, "PromptTokens should be Input+CacheRead")
	require.Equal(t, int64(20), sess.CompletionTokens, "CompletionTokens should be Output")
}
