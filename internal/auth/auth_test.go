package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var secret = []byte("test-secret")

func TestPasswordRoundtrip(t *testing.T) {
	hash, err := HashPassword("password123")
	require.NoError(t, err)
	assert.True(t, CheckPassword(hash, "password123"))
	assert.False(t, CheckPassword(hash, "wrong"))
}

func TestTokenRoundtrip(t *testing.T) {
	id := Identity{UserID: "u1", OrgID: "o1", Role: "admin"}
	access, refresh, err := NewPair(secret, id)
	require.NoError(t, err)

	got, err := Parse(secret, access, KindAccess)
	require.NoError(t, err)
	assert.Equal(t, id, got)

	got, err = Parse(secret, refresh, KindRefresh)
	require.NoError(t, err)
	assert.Equal(t, id, got)
}

func TestParseRejectsWrongKind(t *testing.T) {
	access, refresh, err := NewPair(secret, Identity{UserID: "u1"})
	require.NoError(t, err)

	_, err = Parse(secret, access, KindRefresh)
	assert.ErrorIs(t, err, ErrInvalidToken)
	_, err = Parse(secret, refresh, KindAccess)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestParseRejectsWrongSecret(t *testing.T) {
	access, _, err := NewPair(secret, Identity{UserID: "u1"})
	require.NoError(t, err)

	_, err = Parse([]byte("other"), access, KindAccess)
	assert.ErrorIs(t, err, ErrInvalidToken)
}
