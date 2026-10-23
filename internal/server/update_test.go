package server

import (
	"github.com/bkcarlos/ProtoWatch/internal/update"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpdateRoutes(t *testing.T) {
	s := New()
	s.Updater = update.New("v0.2.1", func() { t.Error("unexpected restart") })
	h := s.Handler()
	for _, tc := range []struct {
		method, path, body, origin string
		status                     int
		contains                   string
	}{
		{"GET", "/api/update", "", "", 200, `"current":"v0.2.1"`},
		{"POST", "/api/update/install", `{"version":"v9.9.9"}`, "", 409, "check for an available release"},
		{"POST", "/api/update/install", `invalid`, "", 400, "error"},
		{"POST", "/api/update/install", `{"version":"v0.2.2"}`, "https://foreign.example", 403, "error"},
	} {
		t.Run(tc.method+tc.path+tc.origin+tc.body, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.contains) {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
}
