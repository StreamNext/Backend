package tvdb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/StreamNext/Backend/internal/tvdb/stub"
)

var epoch = time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Advance(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

// newClient returns a client for a test server. The server's routes live under /v4, like TheTVDB's.
func newClient(t *testing.T, h http.Handler, apiKey string) (*Client, *clock) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	clk := &clock{t: epoch}
	return New(Config{APIKey: apiKey, BaseURL: srv.URL + "/v4"}, srv.Client(), clk.Now), clk
}

// recorded is a client talking to the stub, which replays real TheTVDB responses.
func recorded(t *testing.T) *Client {
	t.Helper()
	c, _ := newClient(t, stub.Handler(), "test-key")
	return c
}

// fixture reads a recorded file, to compare against what the client returns.
func fixture(t *testing.T, name string, into any) {
	t.Helper()
	raw, err := os.ReadFile("stub/testdata/" + name)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, into))
}

func ctx() context.Context { return context.Background() }

func requireErr(t *testing.T, err, target error, msg ...any) {
	t.Helper()
	require.Error(t, err, msg...)
	assert.ErrorIs(t, err, target, msg...)
}

func contextWithCancel() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}
