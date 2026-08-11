package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	AccessTTL  = 15 * time.Minute
	RefreshTTL = 30 * 24 * time.Hour

	KindAccess  = "access"
	KindRefresh = "refresh"
)

var ErrInvalidToken = errors.New("invalid token")

type Identity struct {
	UserID string
	OrgID  string
	Role   string
}

type tokenClaims struct {
	jwt.RegisteredClaims
	OrgID string `json:"org_id"`
	Role  string `json:"role"`
	Kind  string `json:"kind"`
}

func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func NewPair(secret []byte, id Identity) (access string, refresh string, err error) {
	access, err = sign(secret, id, KindAccess, AccessTTL)
	if err != nil {
		return "", "", err
	}
	refresh, err = sign(secret, id, KindRefresh, RefreshTTL)
	if err != nil {
		return "", "", err
	}
	return access, refresh, nil
}

func Parse(secret []byte, raw, wantKind string) (Identity, error) {
	var claims tokenClaims
	_, err := jwt.ParseWithClaims(raw, &claims, func(t *jwt.Token) (any, error) {
		return secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return Identity{}, ErrInvalidToken
	}
	if claims.Kind != wantKind {
		return Identity{}, ErrInvalidToken
	}
	return Identity{
		UserID: claims.Subject,
		OrgID:  claims.OrgID,
		Role:   claims.Role,
	}, nil
}

func sign(secret []byte, id Identity, kind string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := tokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   id.UserID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
		OrgID: id.OrgID,
		Role:  id.Role,
		Kind:  kind,
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
}
