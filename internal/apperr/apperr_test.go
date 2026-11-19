package apperr

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKindMapping(t *testing.T) {
	cases := []struct {
		kind   Kind
		status int
		code   string
	}{
		{NotFound, http.StatusNotFound, "not_found"},
		{Forbidden, http.StatusForbidden, "forbidden"},
		{Conflict, http.StatusConflict, "conflict"},
		{InvalidArgument, http.StatusBadRequest, "invalid_argument"},
		{Unauthenticated, http.StatusUnauthorized, "unauthenticated"},
		{Unavailable, http.StatusServiceUnavailable, "unavailable"},
		{Internal, http.StatusInternalServerError, "internal"},
	}
	for _, c := range cases {
		assert.Equal(t, c.status, c.kind.Status(), c.code)
		assert.Equal(t, c.code, c.kind.Code())
	}
}

func TestFrom(t *testing.T) {
	typed := New(NotFound, "no such show")
	assert.Same(t, typed, From(typed))
	assert.Same(t, typed, From(fmt.Errorf("wrapped: %w", typed)), "unwraps")
	assert.Equal(t, "no such show", typed.Error())

	got := From(errors.New("pq: password=secret leaked"))
	assert.Equal(t, Internal, got.Kind)
	assert.Equal(t, "internal error", got.Message, "untyped errors must not leak details")
}
