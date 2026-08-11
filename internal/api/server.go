package api

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/miroslavrov/be-testcase-2026/internal/config"
	"github.com/miroslavrov/be-testcase-2026/internal/tasks"
)

type Server struct {
	cfg   config.Config
	pool  *pgxpool.Pool
	tasks *tasks.Service
}

func New(cfg config.Config, pool *pgxpool.Pool) *Server {
	return &Server{
		cfg:   cfg,
		pool:  pool,
		tasks: tasks.NewService(pool),
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)

	mux.HandleFunc("POST /v1/auth/token", s.handleAuthToken)
	mux.HandleFunc("POST /v1/auth/refresh", s.handleAuthRefresh)

	mux.HandleFunc("POST /v1/tasks", s.requireAuth(s.handleCreateTask))
	mux.HandleFunc("GET /v1/tasks", s.requireAuth(s.handleListTasks))
	mux.HandleFunc("GET /v1/tasks/{id}", s.requireAuth(s.handleGetTask))
	mux.HandleFunc("POST /v1/tasks/{id}/cancel", s.requireAuth(s.handleCancelTask))

	return mux
}
