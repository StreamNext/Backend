package rest

import (
	"encoding/json"
	"net/http"

	"github.com/StreamNext/Backend/internal/apperr"
)

const maxBodyBytes = 64 << 10

// decode reads a JSON request body into v. On failure it writes the 400 itself and returns false.
func (s *Server) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		s.writeError(w, apperr.New(apperr.InvalidArgument, "request body must be valid JSON matching the contract"))
		return false
	}
	return true
}

// decodeOptional is decode for a body the client may leave out entirely (an empty body counts as "{}").
func (s *Server) decodeOptional(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Body == nil || r.Body == http.NoBody || r.ContentLength == 0 {
		return true
	}
	return s.decode(w, r, v)
}

// orZero reads an optional field from the generated models (pointers when not required).
func orZero[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
