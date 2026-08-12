package api

import (
	"encoding/json"
	"net/http"
	"time"
)

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
