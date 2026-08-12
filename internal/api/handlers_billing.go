package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/miroslavrov/be-testcase-2026/internal/billing"
)

func (s *Server) handleGetSubscription(w http.ResponseWriter, r *http.Request) {
	id := identityFrom(r.Context())
	sub, err := s.billing.Subscription(r.Context(), id.OrgID)
	if errors.Is(err, billing.ErrNoSubscription) {
		writeError(w, http.StatusNotFound, "no_subscription", "organization has no active subscription")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "something went wrong")
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

func (s *Server) handleListInvoices(w http.ResponseWriter, r *http.Request) {
	id := identityFrom(r.Context())
	items, err := s.billing.ListInvoices(r.Context(), id.OrgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "something went wrong")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invoices": items})
}

func (s *Server) handleGetInvoice(w http.ResponseWriter, r *http.Request) {
	id := identityFrom(r.Context())
	inv, err := s.billing.GetInvoice(r.Context(), id.OrgID, r.PathValue("id"))
	if errors.Is(err, billing.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "invoice not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "something went wrong")
		return
	}
	writeJSON(w, http.StatusOK, inv)
}

func (s *Server) handleGenerateInvoice(w http.ResponseWriter, r *http.Request) {
	id := identityFrom(r.Context())

	var body struct {
		Period string `json:"period"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	// период задаётся как "2026-08", по умолчанию текущий месяц
	start := time.Now().UTC()
	if body.Period != "" {
		parsed, err := time.Parse("2006-01", body.Period)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "period must look like 2026-08")
			return
		}
		start = parsed
	}
	periodStart := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	periodEnd := periodStart.AddDate(0, 1, 0)

	inv, created, err := s.billing.Generate(r.Context(), id.OrgID, periodStart, periodEnd)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "something went wrong")
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, inv)
}
