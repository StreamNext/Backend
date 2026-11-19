// Package apperr defines the typed errors services return and the single mapping to HTTP.
package apperr

import (
	"errors"
	"net/http"
)

type Kind int

const (
	Internal Kind = iota
	NotFound
	Forbidden
	Conflict
	InvalidArgument
	Unauthenticated
	Unavailable
	MethodNotAllowed
	TooManyRequests
)

// Error carries a Kind and a client-safe message.
type Error struct {
	Kind    Kind
	Message string
}

func (e *Error) Error() string { return e.Message }

func New(k Kind, msg string) *Error { return &Error{Kind: k, Message: msg} }

// From returns err as an *Error, wrapping anything untyped as Internal with a generic message
// so internal details never reach clients.
func From(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Kind: Internal, Message: "internal error"}
}

// Status is the HTTP status for the kind.
func (k Kind) Status() int {
	switch k {
	case NotFound:
		return http.StatusNotFound
	case Forbidden:
		return http.StatusForbidden
	case Conflict:
		return http.StatusConflict
	case InvalidArgument:
		return http.StatusBadRequest
	case Unauthenticated:
		return http.StatusUnauthorized
	case Unavailable:
		return http.StatusServiceUnavailable
	case MethodNotAllowed:
		return http.StatusMethodNotAllowed
	case TooManyRequests:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

// Code is the stable machine-readable code sent in the error body.
func (k Kind) Code() string {
	switch k {
	case NotFound:
		return "not_found"
	case Forbidden:
		return "forbidden"
	case Conflict:
		return "conflict"
	case InvalidArgument:
		return "invalid_argument"
	case Unauthenticated:
		return "unauthenticated"
	case Unavailable:
		return "unavailable"
	case MethodNotAllowed:
		return "method_not_allowed"
	case TooManyRequests:
		return "rate_limited"
	default:
		return "internal"
	}
}
