package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRefreshToken(t *testing.T) {
	raw, hash, err := NewRefreshToken()
	require.NoError(t, err)
	assert.Len(t, raw, 43, "32 bytes in unpadded base64url")
	assert.Len(t, hash, 64, "SHA-256 in hex")
	assert.Equal(t, hash, HashToken(raw))
	assert.NotContains(t, hash, raw)

	raw2, hash2, _ := NewRefreshToken()
	assert.NotEqual(t, raw, raw2)
	assert.NotEqual(t, hash, hash2)
}

func TestHashTokenIsDeterministic(t *testing.T) {
	assert.Equal(t, HashToken("abc"), HashToken("abc"))
	assert.NotEqual(t, HashToken("abc"), HashToken("abd"))
	// Known SHA-256 vector for "abc".
	assert.Equal(t, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", HashToken("abc"))
}
