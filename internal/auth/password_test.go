package auth

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Cheap parameters keep the tests fast; production uses DefaultPasswordParams.
var testParams = PasswordParams{MemoryKiB: 8, Iterations: 1, Parallelism: 1}

func TestHashAndVerify(t *testing.T) {
	h := NewHasher(testParams)
	phc, err := h.Hash("correct horse battery")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(phc, "$argon2id$v=19$m=8,t=1,p=1$"))

	ok, err := h.Verify("correct horse battery", phc)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = h.Verify("wrong password!!", phc)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestHashIsSalted(t *testing.T) {
	h := NewHasher(testParams)
	a, _ := h.Hash("same password")
	b, _ := h.Hash("same password")
	assert.NotEqual(t, a, b, "two hashes of one password must differ")
}

func TestVerifyUsesParametersStoredInTheHash(t *testing.T) {
	phc, err := NewHasher(testParams).Hash("pw-with-old-params")
	require.NoError(t, err)
	// A hasher configured with different (newer) parameters must still verify an older hash.
	ok, err := NewHasher(DefaultPasswordParams).Verify("pw-with-old-params", phc)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestVerifyRejectsMalformedHashes(t *testing.T) {
	h := NewHasher(testParams)
	for name, phc := range map[string]string{
		"empty":         "",
		"wrong algo":    "$bcrypt$v=19$m=8,t=1,p=1$c2FsdA$a2V5",
		"wrong version": "$argon2id$v=18$m=8,t=1,p=1$c2FsdA$a2V5",
		"bad params":    "$argon2id$v=19$m=x$c2FsdA$a2V5",
		"bad salt":      "$argon2id$v=19$m=8,t=1,p=1$!!!$a2V5",
		"bad key":       "$argon2id$v=19$m=8,t=1,p=1$c2FsdA$!!!",
		"empty key":     "$argon2id$v=19$m=8,t=1,p=1$c2FsdA$",
		"too few parts": "$argon2id$v=19",
	} {
		ok, err := h.Verify("whatever", phc)
		assert.Error(t, err, name)
		assert.False(t, ok, name)
	}
}

func TestDummyDoesNotPanicAndIsReusable(t *testing.T) {
	h := NewHasher(testParams)
	h.Dummy("anything")
	h.Dummy("anything else")
	assert.NotEmpty(t, h.dummyHash)
}
