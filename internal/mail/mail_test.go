package mail

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type capture struct {
	sent []Message
	err  error
}

func (c *capture) Send(_ context.Context, m Message) error {
	c.sent = append(c.sent, m)
	return c.err
}

func TestVerificationEmail(t *testing.T) {
	c := &capture{}
	err := New(c).SendVerification(context.Background(), "ana@example.com", "Ana", "https://app.test/verify-email?token=abc")
	require.NoError(t, err)

	require.Len(t, c.sent, 1)
	m := c.sent[0]
	assert.Equal(t, "ana@example.com", m.To)
	assert.Equal(t, "Verify your StreamNext email", m.Subject)
	assert.Contains(t, m.Text, "Hi Ana,")
	assert.Contains(t, m.Text, "https://app.test/verify-email?token=abc")
	assert.Contains(t, m.Text, "24 hours")
}

func TestPasswordResetEmail(t *testing.T) {
	c := &capture{}
	require.NoError(t, New(c).SendPasswordReset(context.Background(), "ana@example.com", "Ana", "https://app.test/reset-password?token=xyz"))

	m := c.sent[0]
	assert.Equal(t, "Reset your StreamNext password", m.Subject)
	assert.Contains(t, m.Text, "https://app.test/reset-password?token=xyz")
	assert.Contains(t, m.Text, "1 hour")
	assert.Contains(t, m.Text, "your password hasn't changed")
	assert.NotContains(t, m.Subject, "Ana", "nothing user-supplied goes into the subject line")
}

func TestTransportErrorsReachTheCaller(t *testing.T) {
	boom := errors.New("provider down")
	c := &capture{err: boom}
	assert.ErrorIs(t, New(c).SendVerification(context.Background(), "a@b.co", "A", "l"), boom)
	assert.ErrorIs(t, New(c).SendPasswordReset(context.Background(), "a@b.co", "A", "l"), boom)
}

func TestLogTransportWritesTheEmailToTheLog(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	require.NoError(t, NewLogTransport(zap.New(core)).Send(context.Background(), Message{To: "a@b.co", Subject: "S", Text: "link: https://x"}))

	require.Equal(t, 1, logs.Len())
	fields := logs.All()[0].ContextMap()
	assert.Equal(t, "a@b.co", fields["to"])
	assert.Equal(t, "link: https://x", fields["text"])
}

func resendAgainst(srv *httptest.Server) *Resend {
	r := NewResend("re_secret_key", "StreamNext <hi@streamnext.app>")
	r.baseURL = srv.URL
	return r
}

func TestResendSendsTheDocumentedRequest(t *testing.T) {
	var gotAuth, gotType, gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotType, gotPath, gotMethod = r.Header.Get("Authorization"), r.Header.Get("Content-Type"), r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"id":"abc"}`))
	}))
	defer srv.Close()

	err := resendAgainst(srv).Send(context.Background(), Message{To: "ana@example.com", Subject: "Hello", Text: "Body"})
	require.NoError(t, err)
	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/emails", gotPath)
	assert.Equal(t, "Bearer re_secret_key", gotAuth)
	assert.Equal(t, "application/json", gotType)
	assert.Equal(t, map[string]any{"from": "StreamNext <hi@streamnext.app>", "to": []any{"ana@example.com"}, "subject": "Hello", "text": "Body"}, gotBody)
}

func TestResendReportsProviderErrorsWithoutLeakingTheKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"message":"domain not verified"}`)
	}))
	defer srv.Close()

	err := resendAgainst(srv).Send(context.Background(), Message{To: "a@b.co", Subject: "S", Text: "T"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "422")
	assert.Contains(t, err.Error(), "domain not verified")
	assert.NotContains(t, err.Error(), "re_secret_key")
}

func TestResendLimitsHowMuchOfAnErrorBodyItKeeps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, strings.Repeat("x", 10_000))
	}))
	defer srv.Close()
	err := resendAgainst(srv).Send(context.Background(), Message{To: "a@b.co"})
	require.Error(t, err)
	assert.Less(t, len(err.Error()), 700)
}

func TestResendNetworkAndContextErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r := resendAgainst(srv)
	srv.Close() // nothing is listening any more
	assert.ErrorContains(t, r.Send(context.Background(), Message{To: "a@b.co"}), "resend request failed")

	slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body) // the server only notices a client that hung up once the body is read
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer slow.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	assert.Error(t, resendAgainst(slow).Send(ctx, Message{To: "a@b.co"}), "the caller's context cancels the request")

	bad := NewResend("k", "f")
	bad.baseURL = "http://bad host" // not a valid URL
	assert.Error(t, bad.Send(context.Background(), Message{To: "a@b.co"}))
}
