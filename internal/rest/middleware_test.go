package rest

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPanicIsRecoveredAs500(t *testing.T) {
	s := newHarness(t).srv
	h := s.recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	rec := do(t, h, http.MethodGet, "/")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, `{"code":"internal","message":"internal error"}`, rec.Body.String())
}

func TestRequestsAreLoggedWithTheirStatus(t *testing.T) {
	s := newHarness(t).srv
	h := s.logging(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }))
	assert.Equal(t, http.StatusTeapot, do(t, h, http.MethodGet, "/brew").Code, "the wrapper passes the status through")
}
