package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Shared helpers for the tests in this package.
type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

func do(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}
