package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeEmail(t *testing.T) {
	got, err := normalizeEmail("  Ana.Pérez+tag@Example.CO ")
	require.NoError(t, err)
	assert.Equal(t, "ana.pérez+tag@example.co", got)

	for _, bad := range []string{"", "plain", "a@", "@b.co", "a@b", "a b@c.co", "<a@b.co>", "Ana <a@b.co>"} {
		_, err := normalizeEmail(bad)
		assert.Error(t, err, bad)
	}
	long := string(make([]byte, 250)) + "@b.co"
	_, err = normalizeEmail(long)
	assert.Error(t, err, "over 254 characters")
}

func TestDefaultDisplayName(t *testing.T) {
	assert.Equal(t, "ana", defaultDisplayName("ana@example.com"))
	// Multi-byte characters first, local part well over 40 characters.
	name := defaultDisplayName("áéíóú" + "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz@x.co")
	assert.Len(t, []rune(name), 40, "cut to 40 characters, not bytes")
}

func TestNormalizeTimezone(t *testing.T) {
	for in, want := range map[string]string{"": "UTC", "  ": "UTC", "UTC": "UTC", " America/Bogota ": "America/Bogota"} {
		got, err := normalizeTimezone(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got)
	}
	for _, bad := range []string{"Local", "Mars/Olympus", "../etc/passwd"} {
		_, err := normalizeTimezone(bad)
		assert.Error(t, err, bad)
	}
}

func TestPasswordLengthCountsCharactersNotBytes(t *testing.T) {
	assert.Error(t, validatePassword("ñññññññññ"), "9 characters (18 bytes) is too short")
	assert.NoError(t, validatePassword("ñññññññññ"+"ñ"), "10 characters is enough")
	assert.NoError(t, validatePassword(string(make([]rune, 128))))
	assert.Error(t, validatePassword(string(make([]rune, 129))))
}
