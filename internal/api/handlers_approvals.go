package api

import (
	"errors"
	"net/http"

	"github.com/miroslavrov/be-testcase-2026/internal/approvals"
)

func (s *Server) handleListApprovals(w http.ResponseWriter, r *http.Request) {
	id := identityFrom(r.Context())
	items, err := s.approvals.List(r.Context(), id.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "something went wrong")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"approvals": items})
}

func (s *Server) handleGetApproval(w http.ResponseWriter, r *http.Request) {
	id := identityFrom(r.Context())
	detail, err := s.approvals.Get(r.Context(), id.OrgID, r.PathValue("id"))
	if errors.Is(err, approvals.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "approval not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "something went wrong")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}
