// Package auth holds the cryptographic building blocks: password hashing (this file),
// access JWTs (jwt.go) and opaque refresh tokens (tokens.go). It knows nothing about HTTP or the DB.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

// PasswordParams are the argon2id cost settings.
type PasswordParams struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
}

// DefaultPasswordParams is the OWASP minimum for argon2id (19 MiB, 2 iterations, 1 lane).
var DefaultPasswordParams = PasswordParams{MemoryKiB: 19456, Iterations: 2, Parallelism: 1}

const (
	saltLen = 16
	keyLen  = 32
)

// Hasher hashes and verifies passwords as argon2id PHC strings:
// $argon2id$v=19$m=19456,t=2,p=1$<salt>$<key>
type Hasher struct {
	params PasswordParams

	dummyOnce sync.Once
	dummyHash string
}

func NewHasher(p PasswordParams) *Hasher { return &Hasher{params: p} }

func (h *Hasher) Hash(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, h.params.Iterations, h.params.MemoryKiB, h.params.Parallelism, keyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.params.MemoryKiB, h.params.Iterations, h.params.Parallelism,
		enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// Verify reports whether password matches the PHC string. A malformed hash is an error,
// a wrong password is (false, nil).
func (h *Hasher) Verify(password, phc string) (bool, error) {
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("auth: not an argon2id hash")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errors.New("auth: unsupported argon2 version")
	}
	var p PasswordParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.MemoryKiB, &p.Iterations, &p.Parallelism); err != nil {
		return false, errors.New("auth: bad argon2 parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, errors.New("auth: bad salt")
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, errors.New("auth: bad key")
	}
	got := argon2.IDKey([]byte(password), salt, p.Iterations, p.MemoryKiB, p.Parallelism, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// Dummy does the same work as Verify against a throwaway hash. Login calls it for unknown
// emails so response time doesn't reveal which accounts exist.
func (h *Hasher) Dummy(password string) {
	h.dummyOnce.Do(func() { h.dummyHash, _ = h.Hash("streamnext-dummy") })
	_, _ = h.Verify(password, h.dummyHash)
}
