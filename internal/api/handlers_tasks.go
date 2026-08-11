package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/miroslavrov/be-testcase-2026/internal/tasks"
)

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	id := identityFrom(r.Context())

	var in tasks.CreateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid json body")
		return
	}

	task, err := s.tasks.Create(r.Context(), id.OrgID, id.UserID, in)
	if err != nil {
		writeTaskError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func writeTaskError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tasks.ErrValidation):
		writeError(w, http.StatusUnprocessableEntity, "validation", err.Error())
	case errors.Is(err, tasks.ErrUnknownTool):
		writeError(w, http.StatusUnprocessableEntity, "unknown_tool", err.Error())
	case errors.Is(err, tasks.ErrNoSubscription):
		writeError(w, http.StatusPaymentRequired, "no_subscription", "organization has no active subscription")
	case errors.Is(err, tasks.ErrBudgetExceeded):
		writeError(w, http.StatusPaymentRequired, "budget_exceeded", "monthly budget would be exceeded")
	default:
		writeError(w, http.StatusInternalServerError, "internal", "something went wrong")
	}
}
