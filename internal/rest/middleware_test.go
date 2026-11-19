package rest

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestPanicIsRecoveredAs500(t *testing.T) {
	s := NewServer(fakeDB{}, zap.NewNop())
	h := s.recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	rec := do(t, h, http.MethodGet, "/")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, `{"code":"internal","message":"internal error"}`, rec.Body.String())
}
