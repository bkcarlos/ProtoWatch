package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bkcarlos/ProtoWatch/internal/capture"
)

// The directory is configured by the operator before serving requests.
func (s *Server) SetPCAPDir(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(absolute)
	if err != nil {
		return err
	}
	root.Close()
	s.PCAPDir = absolute
	return nil
}

type localFile struct {
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

func pcapName(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return (ext == ".pcap" || ext == ".cap") && name == filepath.Base(name) && !strings.ContainsAny(name, `/\`)
}

func (s *Server) fileRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/files", func(w http.ResponseWriter, r *http.Request) {
		root, err := os.OpenRoot(s.PCAPDir)
		if err != nil {
			fail(w, 400, err)
			return
		}
		defer root.Close()
		dir, err := root.Open(".")
		if err != nil {
			fail(w, 400, err)
			return
		}
		defer dir.Close()
		files := []localFile{}
		truncated := false
		scanned := 0
	scan:
		for {
			entries, err := dir.ReadDir(256)
			if err != nil && err != io.EOF {
				fail(w, 500, err)
				return
			}
			for _, entry := range entries {
				scanned++
				if scanned > 10000 {
					truncated = true
					break scan
				}
				if !pcapName(entry.Name()) || !entry.Type().IsRegular() {
					continue
				}
				info, e := entry.Info()
				if e != nil || !info.Mode().IsRegular() {
					continue
				}
				if len(files) >= 1000 {
					truncated = true
					break scan
				}
				files = append(files, localFile{entry.Name(), info.Size(), info.ModTime()})
			}
			if err == io.EOF {
				break
			}
		}
		sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
		writeJSON(w, map[string]any{"directory": s.PCAPDir, "files": files, "truncated": truncated})
	})
	m.HandleFunc("POST /api/files/open", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			fail(w, 400, err)
			return
		}
		if !pcapName(body.Name) {
			fail(w, 400, fmt.Errorf("请选择服务目录中的 .pcap 或 .cap 文件"))
			return
		}
		root, err := os.OpenRoot(s.PCAPDir)
		if err != nil {
			fail(w, 400, err)
			return
		}
		defer root.Close()
		info, err := root.Lstat(body.Name)
		if err != nil || !info.Mode().IsRegular() {
			fail(w, 400, fmt.Errorf("文件不存在或不是普通文件"))
			return
		}
		f, err := root.Open(body.Name)
		if err != nil {
			fail(w, 400, err)
			return
		}
		if err = s.startFile(f, filepath.Join(s.PCAPDir, body.Name)); err != nil {
			fail(w, 400, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusAccepted)
		writeJSON(w, map[string]bool{"ok": true})
	})
}

// StartFile starts a trusted CLI path asynchronously, so the UI remains available.
func (s *Server) StartFile(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("--read requires a regular PCAP file")
	}
	f, err := os.Open(absolute)
	if err != nil {
		return err
	}
	return s.startFile(f, absolute)
}

// startFile takes ownership of f on both success and failure.
func (s *Server) startFile(f *os.File, name string) error {
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return fmt.Errorf("PCAP requires a regular file")
	}
	ctx, cancel := context.WithCancel(context.Background())
	counter := &importReader{Reader: f, ctx: ctx}
	reader, err := capture.StreamReader(counter)
	if err != nil {
		cancel()
		f.Close()
		return err
	}
	run, err := s.prepareImport(ctx, cancel, counter, reader, name, info.Size(), f.Close)
	if err != nil {
		cancel()
		f.Close()
		return err
	}
	go func() { defer cancel(); defer f.Close(); _ = run() }()
	return nil
}
