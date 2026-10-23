package server

import (
	"encoding/json"
	"net/http"
)

func (s *Server) updateRoutes(m *http.ServeMux) {
	if s.Updater == nil {
		return
	}
	m.HandleFunc("GET /api/update", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.Updater.Status()) })
	m.HandleFunc("POST /api/update/check", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Updater.Check(r.Context()); err != nil {
			fail(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, s.Updater.Status())
	})
	m.HandleFunc("POST /api/update/install", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Version string `json:"version"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			fail(w, 400, err)
			return
		}
		if err := s.Updater.Start(body.Version); err != nil {
			fail(w, http.StatusConflict, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		writeJSON(w, s.Updater.Status())
	})
}
