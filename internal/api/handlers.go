package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/RedBoardDev/gh-runners-tool/v2/internal/model"
)

type statusResponse struct {
	Groups map[string][]model.RunnerSnapshot `json:"groups"`
	Health healthResponse                    `json:"health"`
}

type healthResponse struct {
	LastCheck time.Time           `json:"last_check"`
	Issues    []model.HealthIssue `json:"issues"`
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", s.handleStatus)
	mux.HandleFunc("GET /health", s.handleHealth)
	return mux
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	snapshots := s.controller.Snapshots()
	hs := s.health.Status()

	resp := statusResponse{
		Groups: snapshots,
		Health: healthResponse{
			LastCheck: hs.LastCheck,
			Issues:    hs.Issues,
		},
	}

	writeJSON(w, resp)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	hs := s.health.Status()

	resp := healthResponse{
		LastCheck: hs.LastCheck,
		Issues:    hs.Issues,
	}

	writeJSON(w, resp)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")

	data, err := json.Marshal(v)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	_, writeErr := w.Write(data)
	if writeErr != nil {
		return
	}
}
