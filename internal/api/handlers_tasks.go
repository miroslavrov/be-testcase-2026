package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/miroslavrov/be-testcase-2026/internal/tasks"
)

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	id := identityFrom(r.Context())

	var in tasks.CreateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
		return
	}

	task, err := s.tasks.Create(r.Context(), id.OrgID, id.UserID, r.Header.Get("Idempotency-Key"), in)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	id := identityFrom(r.Context())

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	filter := tasks.ListFilter{
		Status: r.URL.Query().Get("status"),
		Limit:  limit,
		Offset: offset,
	}

	items, err := s.tasks.List(r.Context(), id.OrgID, filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "something went wrong")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": items})
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := identityFrom(r.Context())

	detail, err := s.tasks.Get(r.Context(), id.OrgID, r.PathValue("id"))
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	id := identityFrom(r.Context())

	if err := s.tasks.Cancel(r.Context(), id.OrgID, id.UserID, r.PathValue("id")); err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func writeTaskError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tasks.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "task not found")
	case errors.Is(err, tasks.ErrNotCancellable):
		writeError(w, http.StatusConflict, "not_cancellable", "task is already finished")
	case errors.Is(err, tasks.ErrValidation):
		writeError(w, http.StatusUnprocessableEntity, "validation", err.Error())
	case errors.Is(err, tasks.ErrUnknownTool):
		writeError(w, http.StatusUnprocessableEntity, "unknown_tool", err.Error())
	case errors.Is(err, tasks.ErrNoSubscription):
		writeError(w, http.StatusPaymentRequired, "no_subscription", "organization has no active subscription")
	case errors.Is(err, tasks.ErrBudgetExceeded):
		writeError(w, http.StatusPaymentRequired, "budget_exceeded", "monthly budget would be exceeded")
	case errors.Is(err, tasks.ErrIdempotencyMismatch):
		writeError(w, http.StatusUnprocessableEntity, "idempotency_mismatch", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal", "something went wrong")
	}
}
