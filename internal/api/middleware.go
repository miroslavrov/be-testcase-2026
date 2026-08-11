package api

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/miroslavrov/be-testcase-2026/internal/auth"
)

type ctxKey int

const identityKey ctxKey = iota

func (s *Server) requireAuth(next http.HandlerFunc, roles ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || raw == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "bearer token is required")
			return
		}
		id, err := auth.Parse([]byte(s.cfg.JWTSecret), raw, auth.KindAccess)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "token is invalid or expired")
			return
		}
		if len(roles) > 0 && !slices.Contains(roles, id.Role) {
			writeError(w, http.StatusForbidden, "forbidden", "insufficient role")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), identityKey, id)))
	}
}

func identityFrom(ctx context.Context) auth.Identity {
	id, _ := ctx.Value(identityKey).(auth.Identity)
	return id
}
