package api

import (
	"encoding/json"
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

func (s *Server) handleApproveApproval(w http.ResponseWriter, r *http.Request) {
	s.decideApproval(w, r, true)
}

func (s *Server) handleRejectApproval(w http.ResponseWriter, r *http.Request) {
	s.decideApproval(w, r, false)
}

func (s *Server) decideApproval(w http.ResponseWriter, r *http.Request, approve bool) {
	id := identityFrom(r.Context())

	var body struct {
		Comment string `json:"comment"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	var err error
	if approve {
		err = s.approvals.Approve(r.Context(), id.OrgID, r.PathValue("id"), id.UserID, id.Role, body.Comment)
	} else {
		err = s.approvals.Reject(r.Context(), id.OrgID, r.PathValue("id"), id.UserID, id.Role, body.Comment)
	}

	switch {
	case errors.Is(err, approvals.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "approval not found")
	case errors.Is(err, approvals.ErrNotActionable):
		writeError(w, http.StatusConflict, "not_actionable", "approval already resolved")
	case errors.Is(err, approvals.ErrWrongRole):
		writeError(w, http.StatusForbidden, "wrong_role", "your role cannot act on the current step")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal", "something went wrong")
	default:
		action := "approved"
		if !approve {
			action = "rejected"
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": action})
	}
}
