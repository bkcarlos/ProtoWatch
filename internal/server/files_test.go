package server

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServerFileCatalogAndOpen(t *testing.T) {
	dir := t.TempDir()
	s := New()
	if err := s.SetPCAPDir(dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("web/mixed-demo.pcap")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"capture.pcap", "upper.CAP", "notes.txt"} {
		if err = os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.Mkdir(filepath.Join(dir, "folder.pcap"), 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.pcap")
	if err = os.WriteFile(outside, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, filepath.Join(dir, "escape.pcap")); err != nil {
		t.Fatal(err)
	}
	handler := s.Handler()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/files", nil))
	var catalog struct {
		Directory string      `json:"directory"`
		Files     []localFile `json:"files"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || catalog.Directory != dir || len(catalog.Files) != 2 || catalog.Files[0].Name != "capture.pcap" {
		t.Fatal(w.Code, w.Body.String())
	}
	open := func(name string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"name": name})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("POST", "/api/files/open", bytes.NewReader(body)))
		return w
	}
	s.MaxImportBytes = 1 // Server-side files must not inherit the browser upload limit.
	w = open("capture.pcap")
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("background import did not finish")
	}
	state := s.Store.Snapshot()
	if state.Status != "complete" || state.SampleCount != 120 || state.SomeIP.MessageCount != 93 || state.ImportBytes != int64(len(data)) {
		t.Fatal(state.Status, state.Packets)
	}
	for _, name := range []string{"../outside.pcap", outside, "escape.pcap", "folder.pcap", "notes.txt", "missing.pcap", `sub\file.pcap`} {
		if w = open(name); w.Code != 400 {
			t.Fatalf("accepted %q", name)
		}
	}
	if err = os.WriteFile(filepath.Join(dir, "bad.pcap"), []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	if w = open("bad.pcap"); w.Code != 400 || s.Store.Snapshot().Started != state.Started {
		t.Fatal("bad header replaced session")
	}
}

func TestBackgroundFileCanBeStoppedAndReservesSession(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "large-*.pcap")
	if err != nil {
		t.Fatal(err)
	}
	input, _ := pcapStream(0, 1024*1024)
	if _, err = io.Copy(f, input); err != nil {
		t.Fatal(err)
	}
	record := make([]byte, 16)
	binary.LittleEndian.PutUint32(record[8:], 1024*1024)
	binary.LittleEndian.PutUint32(record[12:], 1024*1024)
	for i := 0; i < 512; i++ {
		if _, err = f.Write(record); err != nil {
			t.Fatal(err)
		}
		if _, err = f.Seek(1024*1024, io.SeekCurrent); err != nil {
			t.Fatal(err)
		}
	}
	size, _ := f.Seek(0, io.SeekCurrent)
	if err = f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	f.Close()
	s := New()
	if err = s.StartFile(f.Name()); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	reserved := s.busy()
	s.mu.Unlock()
	if !reserved {
		t.Fatal("file session not reserved before return")
	}
	data, _ := os.ReadFile("web/demo.pcap")
	if err = os.WriteFile(filepath.Join(filepath.Dir(f.Name()), "second.pcap"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.StartFile(filepath.Join(filepath.Dir(f.Name()), "second.pcap")); err == nil {
		t.Fatal("replaced active import")
	}
	s.Stop()
	if state := s.Store.Snapshot(); state.Status != "stopped" || state.ImportBytes >= size {
		t.Fatal(state.Status, state.ImportBytes, size)
	}
	if err = s.StartFile(filepath.Join(filepath.Dir(f.Name()), "second.pcap")); err != nil {
		t.Fatal(err)
	}
	s.Stop()
}
