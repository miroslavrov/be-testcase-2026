package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/miroslavrov/be-testcase-2026/internal/auth"
)

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

func (s *Server) handleAuthToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "email and password are required")
		return
	}

	var (
		id   auth.Identity
		hash string
	)
	err := s.pool.QueryRow(r.Context(),
		`select id, org_id, role, password_hash from users where email = $1`,
		req.Email).Scan(&id.UserID, &id.OrgID, &id.Role, &hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "wrong email or password")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "something went wrong")
		return
	}
	if !auth.CheckPassword(hash, req.Password) {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "wrong email or password")
		return
	}

	s.respondTokens(w, id)
}

func (s *Server) handleAuthRefresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "refresh_token is required")
		return
	}

	parsed, err := auth.Parse([]byte(s.cfg.JWTSecret), req.RefreshToken, auth.KindRefresh)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_token", "refresh token is invalid or expired")
		return
	}

	// перечитываем юзера из базы, вдруг роль поменялась пока токен жил
	var id auth.Identity
	err = s.pool.QueryRow(r.Context(),
		`select id, org_id, role from users where id = $1`,
		parsed.UserID).Scan(&id.UserID, &id.OrgID, &id.Role)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_token", "user no longer exists")
		return
	}

	s.respondTokens(w, id)
}

func (s *Server) respondTokens(w http.ResponseWriter, id auth.Identity) {
	access, refresh, err := auth.NewPair([]byte(s.cfg.JWTSecret), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "failed to issue tokens")
		return
	}
	writeJSON(w, http.StatusOK, tokenResponse{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresIn:    int(auth.AccessTTL.Seconds()),
	})
}
