package service

import (
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/StreamNext/Backend/internal/apperr"
)

const (
	minPasswordLen  = 10
	maxPasswordLen  = 128
	maxEmailLen     = 254
	maxDisplayLen   = 40
	defaultTimezone = "UTC"
)

func invalid(msg string) error { return apperr.New(apperr.InvalidArgument, msg) }

// normalizeEmail trims and lowercases, and accepts only a bare address (no "Name <a@b>" forms).
func normalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || len(email) > maxEmailLen || !strings.Contains(email[strings.LastIndex(email, "@"):], ".") {
		return "", invalid("email is invalid")
	}
	return email, nil
}

func validatePassword(pw string) error {
	if n := utf8.RuneCountInString(pw); n < minPasswordLen || n > maxPasswordLen {
		return invalid("password must be 10 to 128 characters")
	}
	return nil
}

func normalizeDisplayName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(name); n < 1 || n > maxDisplayLen {
		return "", invalid("displayName must be 1 to 40 characters")
	}
	return name, nil
}

// defaultDisplayName is the part of the email before the @, cut to the maximum length.
func defaultDisplayName(email string) string {
	name := email[:strings.Index(email, "@")]
	if utf8.RuneCountInString(name) > maxDisplayLen {
		name = string([]rune(name)[:maxDisplayLen])
	}
	return name
}

// normalizeTimezone accepts an IANA name; empty means UTC.
func normalizeTimezone(raw string) (string, error) {
	tz := strings.TrimSpace(raw)
	if tz == "" {
		return defaultTimezone, nil
	}
	if tz == "Local" { // time.LoadLocation accepts it, but it means "the server's zone"
		return "", invalid("timezone must be an IANA name such as America/Bogota")
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return "", invalid("timezone must be an IANA name such as America/Bogota")
	}
	return tz, nil
}
