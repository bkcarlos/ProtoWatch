package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestHTTPWorkflowAndAccess(t *testing.T) {
	s := New()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	request := func(method, path string, body io.Reader, origin string) *http.Response {
		t.Helper()
		r, _ := http.NewRequest(method, ts.URL+path, body)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		resp, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		return resp
	}
	r := request("GET", "/api/state", nil, "")
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal("API should be accessible without credentials")
	}
	r = request("POST", "/api/capture/stop", nil, "http://untrusted.example")
	r.Body.Close()
	if r.StatusCode != 403 {
		t.Fatal("cross-origin mutation accepted")
	}
	b, e := os.ReadFile("web/demo.pcap")
	if e != nil {
		t.Fatal(e)
	}
	r = request("POST", "/api/import?name=synthetic.pcap", bytes.NewReader(b), "")
	defer r.Body.Close()
	if r.StatusCode != 200 {
		data, _ := io.ReadAll(r.Body)
		t.Fatalf("import failed: %s", data)
	}
	var result struct {
		SampleCount int `json:"sample_count"`
	}
	if e = json.NewDecoder(r.Body).Decode(&result); e != nil {
		t.Fatal(e)
	}
	if result.SampleCount != 120 {
		t.Fatal(result)
	}
	r = request("GET", "/api/samples/1", nil, "")
	payload, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if r.StatusCode != 200 || !bytes.Contains(payload, []byte("payload_base64")) {
		t.Fatal("sample payload missing")
	}
	r = request("GET", "/api/export", nil, "")
	payload, _ = io.ReadAll(r.Body)
	r.Body.Close()
	if !bytes.Contains(payload, []byte("retained_samples")) {
		t.Fatal("export missing samples")
	}
	r = request("GET", "/", nil, "")
	payload, _ = io.ReadAll(r.Body)
	r.Body.Close()
	if !bytes.Contains(payload, []byte("ProtoWatch")) {
		t.Fatal("embedded UI missing")
	}
	assets := regexp.MustCompile(`(?:src|href)="(/assets/[^" ]+)"`).FindAllSubmatch(payload, -1)
	if len(assets) < 2 {
		t.Fatal("built module script and stylesheet missing")
	}
	for _, asset := range assets {
		path := string(asset[1])
		r = request("GET", path, nil, "")
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		if r.StatusCode != 200 || len(body) == 0 {
			t.Fatalf("embedded asset missing: %s", path)
		}
		contentType := r.Header.Get("Content-Type")
		if strings.HasSuffix(path, ".js") && !strings.Contains(contentType, "javascript") || strings.HasSuffix(path, ".css") && !strings.Contains(contentType, "text/css") {
			t.Fatalf("incorrect asset MIME type: %s: %s", path, contentType)
		}
	}
}
func TestLANAccessWithoutCredentials(t *testing.T) {
	s := New()
	for _, host := range []string{"192.168.1.10:8080", "dds-device.local:8080", "[fd00::10]:8080"} {
		t.Run(host, func(t *testing.T) {
			for _, path := range []string{"/api/state", "/api/capture/stop"} {
				method := "GET"
				if path == "/api/capture/stop" {
					method = "POST"
				}
				w := httptest.NewRecorder()
				r := httptest.NewRequest(method, "http://"+host+path, nil)
				r.Header.Set("Origin", "http://"+host)
				s.Handler().ServeHTTP(w, r)
				if w.Code != 200 {
					t.Fatalf("%s: got %d: %s", path, w.Code, w.Body.String())
				}
			}
		})
	}
}

func TestSomeIPHTTPWorkflow(t *testing.T) {
	s := New()
	handler := s.Handler()
	call := func(method, path string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "http://device.local:8080"+path, bytes.NewReader(body))
		r.Header.Set("Origin", "http://device.local:8080")
		handler.ServeHTTP(w, r)
		return w
	}
	data, err := os.ReadFile("web/mixed-demo.pcap")
	if err != nil {
		t.Fatal(err)
	}
	if w := call("POST", "/api/import?name=mixed", data); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	mapping := []byte(`{"services":{"0x1234":{"name":"Vehicle","methods":{"1":"Read"}}}}`)
	if w := call("PUT", "/api/someip/config", mapping); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := call("PUT", "/api/someip/config", []byte(`{"ports":[70000]}`)); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := call("GET", "/api/someip/config", nil); !bytes.Contains(w.Body.Bytes(), []byte("Vehicle")) {
		t.Fatal("invalid update replaced mapping")
	}
	w := call("GET", "/api/someip/samples/1", nil)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"service_name":"Vehicle"`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"payload_base64":"AAAAAQ=="`)) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call("GET", "/api/someip/samples/999", nil); w.Code != 404 {
		t.Fatal(w.Code)
	}
	w = call("GET", "/api/export", nil)
	var exported map[string]json.RawMessage
	if err = json.Unmarshal(w.Body.Bytes(), &exported); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"state", "retained_samples", "someip_retained_samples", "someip_config"} {
		if len(exported[key]) == 0 {
			t.Fatal("missing export", key)
		}
	}
}
