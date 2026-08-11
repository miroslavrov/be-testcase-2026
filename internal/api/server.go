package api

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/miroslavrov/be-testcase-2026/internal/config"
)

type Server struct {
	cfg  config.Config
	pool *pgxpool.Pool
}

func New(cfg config.Config, pool *pgxpool.Pool) *Server {
	return &Server{cfg: cfg, pool: pool}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)

	mux.HandleFunc("POST /v1/auth/token", s.handleAuthToken)
	mux.HandleFunc("POST /v1/auth/refresh", s.handleAuthRefresh)

	return mux
}
