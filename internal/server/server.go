package server

import (
	"bytes"
	"context"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gopacket/gopacket/pcapgo"

	"github.com/bkcarlos/ProtoWatch/internal/buildinfo"
	"github.com/bkcarlos/ProtoWatch/internal/capture"
	"github.com/bkcarlos/ProtoWatch/internal/someip"
	"github.com/bkcarlos/ProtoWatch/internal/update"
	"github.com/bkcarlos/ProtoWatch/internal/watch"
)

//go:embed web/*
var assets embed.FS

type Server struct {
	// Configure before serving requests.
	Updater        *update.Manager
	MaxImportBytes int64
	PCAPDir        string
	Store          *watch.Store
	mu             sync.Mutex
	cancel         context.CancelFunc
	done           chan struct{}
	closeSource    func() error
	importGate     chan struct{}
}

func New() *Server {
	return &Server{PCAPDir: ".", MaxImportBytes: capture.MaxImport, Store: watch.NewStore(), importGate: make(chan struct{}, 1)}
}

func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	s.fileRoutes(m)
	s.updateRoutes(m)
	m.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.Store.Snapshot()) })
	m.HandleFunc("GET /api/interfaces", func(w http.ResponseWriter, r *http.Request) {
		ifs, err := net.Interfaces()
		if err != nil {
			fail(w, 500, err)
			return
		}
		type iface struct {
			Name      string   `json:"name"`
			Addresses []string `json:"addresses"`
			Up        bool     `json:"up"`
			Loopback  bool     `json:"loopback"`
		}
		out := []iface{}
		for _, i := range ifs {
			addresses := []string{}
			a, _ := i.Addrs()
			for _, v := range a {
				addresses = append(addresses, v.String())
			}
			out = append(out, iface{i.Name, addresses, i.Flags&net.FlagUp != 0, i.Flags&net.FlagLoopback != 0})
		}
		writeJSON(w, map[string]any{"interfaces": out, "live_supported": runtime.GOOS == "linux", "version": buildinfo.Version, "commit": buildinfo.Commit, "max_import_bytes": s.MaxImportBytes})
	})
	m.HandleFunc("POST /api/capture/start", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Interface string `json:"interface"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			fail(w, 400, err)
			return
		}
		if err := s.Start(body.Interface); err != nil {
			fail(w, 409, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	m.HandleFunc("POST /api/capture/stop", func(w http.ResponseWriter, r *http.Request) { s.Stop(); writeJSON(w, map[string]bool{"ok": true}) })
	m.HandleFunc("POST /api/import", func(w http.ResponseWriter, r *http.Request) {
		select {
		case s.importGate <- struct{}{}:
			defer func() { <-s.importGate }()
		default:
			fail(w, 409, fmt.Errorf("已有文件正在上传"))
			return
		}
		if r.ContentLength > s.MaxImportBytes {
			fail(w, 413, fmt.Errorf("PCAP exceeds upload limit of %d MiB", s.MaxImportBytes/(1024*1024)))
			return
		}
		// Large uploads need an inactivity deadline rather than a whole-request timeout.
		rc := http.NewResponseController(w)
		_ = rc.SetWriteDeadline(time.Time{})
		body := http.MaxBytesReader(w, r.Body, s.MaxImportBytes)
		name := r.URL.Query().Get("name")
		if len(name) > 200 {
			name = name[:200]
		}
		if name == "" {
			name = "uploaded.pcap"
		}
		if err := s.importStream(r.Context(), body, name, max(0, r.ContentLength),
			func() { _ = rc.SetReadDeadline(time.Now().Add(2 * time.Minute)) },
			func() error { return rc.SetReadDeadline(time.Now()) }); err != nil {
			code := http.StatusBadRequest
			var limit *http.MaxBytesError
			if errors.As(err, &limit) {
				code = http.StatusRequestEntityTooLarge
			}
			fail(w, code, err)
			return
		}
		writeJSON(w, s.Store.Snapshot())
	})
	m.HandleFunc("GET /api/samples/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
		if err != nil {
			fail(w, 400, err)
			return
		}
		sample, ok := s.Store.Sample(id)
		if !ok {
			fail(w, 404, fmt.Errorf("sample is no longer retained"))
			return
		}
		writeJSON(w, map[string]any{"sample": sample, "hex": hex.Dump(sample.Payload), "payload_base64": sample.Payload, "decode_status": "raw_only"})
	})
	m.HandleFunc("GET /api/someip/samples/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
		if err != nil {
			fail(w, 400, err)
			return
		}
		sample, ok := s.Store.SomeIPSample(id)
		if !ok {
			fail(w, 404, fmt.Errorf("sample is no longer retained"))
			return
		}
		writeJSON(w, map[string]any{"sample": sample, "hex": hex.Dump(sample.Payload), "payload_base64": sample.Payload, "decode_status": "raw_only"})
	})
	m.HandleFunc("GET /api/someip/config", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.Store.SomeIPConfig()) })
	m.HandleFunc("PUT /api/someip/config", func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, someip.MaxConfigBytes))
		if err != nil {
			fail(w, 413, err)
			return
		}
		if err = s.Store.SetSomeIPConfig(data); err != nil {
			fail(w, 400, err)
			return
		}
		writeJSON(w, s.Store.SomeIPConfig())
	})
	m.HandleFunc("GET /api/export", func(w http.ResponseWriter, r *http.Request) {
		v := s.Store.Snapshot()
		type row struct {
			watch.Sample
			Payload []byte `json:"payload_base64"`
		}
		rows := []row{}
		for _, sample := range v.Samples {
			rows = append(rows, row{sample, sample.Payload})
		}
		w.Header().Set("Content-Disposition", `attachment; filename="proto-watch-snapshot.json"`)
		writeJSON(w, map[string]any{"version": buildinfo.Version, "commit": buildinfo.Commit, "note": "Retained samples only; payload previews may be truncated. Not a replayable PCAP.", "state": v, "retained_samples": rows, "someip_config": s.Store.SomeIPConfig(), "someip_retained_samples": someIPExport(v.SomeIP.Samples)})
	})
	web, _ := fs.Sub(assets, "web")
	m.Handle("/", http.FileServer(http.FS(web)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			// API calls from browsers must use the same origin as the page.
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				scheme := "http"
				if r.TLS != nil {
					scheme = "https"
				}
				if err != nil || u.Host != r.Host || u.Scheme != scheme {
					fail(w, 403, fmt.Errorf("cross-origin request rejected"))
					return
				}
			}

		}
		m.ServeHTTP(w, r)
	})
}
func (s *Server) busy() bool {
	if s.done == nil {
		return false
	}
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}
func (s *Server) Start(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy() {
		return fmt.Errorf("已有观察会话运行中，请先停止")
	}
	i, err := net.InterfaceByName(name)
	if err != nil {
		return err
	}
	if i.Flags&net.FlagUp == 0 {
		return fmt.Errorf("interface is down")
	}
	if i.Flags&net.FlagLoopback != 0 || len(i.HardwareAddr) != 6 {
		return fmt.Errorf("MVP live capture requires an Ethernet-compatible interface; loopback is not supported")
	}
	h, err := capture.OpenLive(name)
	if err != nil {
		return fmt.Errorf("capture %s: %w (Linux requires CAP_NET_RAW)", name, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.closeSource = h.Close
	s.done = make(chan struct{})
	done := s.done
	s.Store.Reset(name, "live")
	s.Store.Status("running", nil)
	if n, e := h.Dropped(); e == nil {
		s.Store.Drops(n)
	}
	go func() {
		// Stats reads reset the kernel counter. Accumulate every read in the store.
		statsDone := make(chan struct{})
		go func() {
			defer close(statsDone)
			tick := time.NewTicker(time.Second)
			defer tick.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
					if n, e := h.Dropped(); e == nil {
						s.Store.Drops(n)
					}
				}
			}
		}()
		err := capture.Run(ctx, h, s.Store)
		cancel()
		<-statsDone
		if n, e := h.Dropped(); e == nil {
			s.Store.Drops(n)
		}
		h.Close()
		if err != nil {
			s.Store.Status("error", err)
		} else {
			s.Store.Status("stopped", nil)
		}
		close(done)
	}()
	return nil
}
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.busy() {
		return
	}
	s.cancel()
	if s.closeSource != nil {
		s.closeSource()
	}
	<-s.done
}
func (s *Server) Import(ctx context.Context, data []byte, name string) error {
	return s.ImportReader(ctx, bytes.NewReader(data), name, int64(len(data)))
}

// ImportReader streams local files without an upload-size limit. total is optional progress metadata.
func (s *Server) ImportReader(ctx context.Context, input io.Reader, name string, total int64) error {
	return s.importStream(ctx, input, name, total, nil, nil)
}

func (s *Server) importStream(parent context.Context, input io.Reader, name string, total int64, beforeRead func(), interrupt func() error) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	if interrupt != nil {
		interrupted := make(chan struct{})
		stop := context.AfterFunc(ctx, func() { _ = interrupt(); close(interrupted) })
		defer func() {
			if !stop() {
				<-interrupted
			}
		}()
	}
	counter := &importReader{Reader: input, ctx: ctx, beforeRead: beforeRead}
	r, err := capture.StreamReader(counter)
	if err != nil {
		return err
	}
	run, err := s.prepareImport(ctx, cancel, counter, r, name, total, interrupt)
	if err != nil {
		return err
	}
	return run()
}

// Reserve the session before returning to asynchronous callers.
func (s *Server) prepareImport(ctx context.Context, cancel context.CancelFunc, counter *importReader, r *pcapgo.Reader, name string, total int64, interrupt func() error) (func() error, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy() {
		return nil, fmt.Errorf("已有观察会话运行中，请先停止")
	}
	s.cancel = cancel
	s.closeSource = interrupt
	s.done = make(chan struct{})
	done := s.done
	s.Store.Reset(name, "file")
	s.Store.Status("importing", nil)
	counter.progress = func(n int64) { s.Store.ImportProgress(n, total) }
	counter.progress(counter.bytes)
	return func() error {
		err := capture.Import(ctx, r, s.Store)
		if ctx.Err() != nil {
			err = ctx.Err()
			s.Store.Status("stopped", nil)
		} else if err != nil {
			s.Store.Status("error", err)
		} else {
			s.Store.Status("complete", nil)
		}
		close(done)
		return err
	}, nil
}

type importReader struct {
	io.Reader
	ctx        context.Context
	bytes      int64
	beforeRead func()
	progress   func(int64)
}

func (r *importReader) Read(p []byte) (int, error) {
	if r.beforeRead != nil {
		r.beforeRead()
	}
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.Reader.Read(p)
	r.bytes += int64(n)
	if r.progress != nil {
		r.progress(r.bytes)
	}
	return n, err
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func someIPExport(samples []someip.Sample) []map[string]any {
	out := []map[string]any{}
	for _, sample := range samples {
		out = append(out, map[string]any{"sample": sample, "payload_base64": sample.Payload})
	}
	return out
}
