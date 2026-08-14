package httpapi

import (
	"net/http"

	"capsnap/internal/store"
)

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	query := store.StatsQuery{
		TimeRange: r.URL.Query().Get("range"),
		DeviceID:  r.URL.Query().Get("device_id"),
	}
	if query.TimeRange == "" {
		query.TimeRange = "today"
	}

	stats, err := s.store.GetStats(r.Context(), query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}
