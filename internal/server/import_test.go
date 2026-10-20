package server

import (
	"bytes"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Each reader reuses the same packet bytes; the fixture itself is not file-sized.
func pcapStream(packets, size int) (io.Reader, int64) {
	header := make([]byte, 24)
	binary.LittleEndian.PutUint32(header, 0xa1b2c3d4)
	binary.LittleEndian.PutUint16(header[4:], 2)
	binary.LittleEndian.PutUint16(header[6:], 4)
	binary.LittleEndian.PutUint32(header[16:], uint32(size))
	binary.LittleEndian.PutUint32(header[20:], 1)
	record := make([]byte, 16+size)
	binary.LittleEndian.PutUint32(record, 100)
	binary.LittleEndian.PutUint32(record[8:], uint32(size))
	binary.LittleEndian.PutUint32(record[12:], uint32(size))
	readers := []io.Reader{bytes.NewReader(header)}
	for i := 0; i < packets; i++ {
		readers = append(readers, bytes.NewReader(record))
	}
	return io.MultiReader(readers...), int64(24 + packets*len(record))
}

func TestStreamingUploadAboveOldLimit(t *testing.T) {
	s := New()
	body, size := pcapStream(80, 1024*1024)
	r := httptest.NewRequest("POST", "/api/import?name=large.pcap", body)
	r.ContentLength = size
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	state := s.Store.Snapshot()
	if w.Code != 200 || state.Packets != 80 || state.ImportBytes != size || state.ImportTotal != size || state.Status != "complete" {
		t.Fatalf("large import: HTTP %d, packets %d, bytes %d/%d, status %s", w.Code, state.Packets, state.ImportBytes, size, state.Status)
	}
}

func TestUploadLimitAndTruncation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		limit    int64
		known    bool
		truncate bool
		code     int
		status   string
	}{
		{"exact limit", 100, true, false, 200, "complete"},
		{"known oversize preserves session", 99, true, false, 413, "idle"},
		{"chunked oversize", 99, false, false, 413, "error"},
		{"truncated packet", 100, true, true, 400, "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			s.MaxImportBytes = tc.limit
			body, size := pcapStream(1, 60)
			if tc.truncate {
				body = io.LimitReader(body, size-1)
				size--
			}
			r := httptest.NewRequest("POST", "/api/import", body)
			if tc.known {
				r.ContentLength = size
			} else {
				r.ContentLength = -1
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, r)
			if w.Code != tc.code || s.Store.Snapshot().Status != tc.status {
				t.Fatalf("%d %s %+v", w.Code, w.Body.String(), s.Store.Snapshot())
			}
		})
	}
}

func waitPackets(t *testing.T, s *Server, n uint64) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if s.Store.Snapshot().Packets >= n {
			return
		}
		select {
		case <-deadline:
			t.Fatal("stream was buffered until EOF")
		case <-tick.C:
		}
	}
}

func TestStopInterruptsStalledUpload(t *testing.T) {
	s := New()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	reader, writer := io.Pipe()
	defer writer.Close()
	defer reader.Close()
	req, _ := http.NewRequest("POST", ts.URL+"/api/import", reader)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		response, err := http.DefaultClient.Do(req)
		if err == nil {
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
		}
	}()
	body, _ := pcapStream(1, 60)
	if _, err := io.Copy(writer, body); err != nil {
		t.Fatal(err)
	}
	waitPackets(t, s, 1)
	stopped := make(chan struct{})
	go func() { s.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("stop blocked on upload body")
	}
	writer.Close()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("upload did not return")
	}
	if state := s.Store.Snapshot(); state.Status != "stopped" || state.Packets != 1 {
		t.Fatal(state.Status, state.Packets)
	}
}

func TestUploadOverridesWholeRequestDeadlines(t *testing.T) {
	s := New()
	ts := httptest.NewUnstartedServer(s.Handler())
	ts.Config.ReadTimeout = 50 * time.Millisecond
	ts.Config.WriteTimeout = 50 * time.Millisecond
	ts.Start()
	defer ts.Close()
	reader, writer := io.Pipe()
	defer reader.Close()
	done := make(chan error, 1)
	go func() {
		body, _ := pcapStream(1, 60)
		_, err := io.Copy(writer, body)
		if err == nil {
			time.Sleep(150 * time.Millisecond)
		}
		writer.CloseWithError(err)
		done <- err
	}()
	response, err := http.Post(ts.URL+"/api/import", "application/octet-stream", reader)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		data, _ := io.ReadAll(response.Body)
		t.Fatalf("%d: %s", response.StatusCode, data)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}
