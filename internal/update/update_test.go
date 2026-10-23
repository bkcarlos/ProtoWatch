package update

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// The staged copy of the test binary doubles as a portable release executable.
func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "--hold" {
		fmt.Println("ready")
		time.Sleep(time.Minute)
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("proto-watch v0.2.2 (test)")
		return
	}
	os.Exit(m.Run())
}
func TestVersion(t *testing.T) {
	for _, tt := range []struct {
		latest, current string
		want            bool
	}{
		{"v0.2.2", "0.2.1-mvp", true}, {"v0.2.2", "0.2.2-mvp", true}, {"v0.2.1", "v0.2.1", false}, {"v0.2.1", "0.2.2-dev", false}, {"v0.10.0", "0.9.9", true}, {"v1.0.0-beta", "0.2.1", false}, {"v1.0.0", "development", false}, {"v99999999999999999999999.0.0", "0.2.1", false},
	} {
		if got := newer(tt.latest, tt.current); got != tt.want {
			t.Errorf("newer(%s,%s)=%v", tt.latest, tt.current, got)
		}
	}
}
func pack(t *testing.T, entry string, data []byte, zipFormat, link bool) []byte {
	t.Helper()
	var b bytes.Buffer
	if zipFormat {
		z := zip.NewWriter(&b)
		h := &zip.FileHeader{Name: entry, Method: zip.Deflate}
		h.SetMode(0755)
		if link {
			h.SetMode(os.ModeSymlink | 0755)
		}
		f, e := z.CreateHeader(h)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.Write(data); e != nil {
			t.Fatal(e)
		}
		if e = z.Close(); e != nil {
			t.Fatal(e)
		}
	} else {
		gz := gzip.NewWriter(&b)
		tr := tar.NewWriter(gz)
		h := &tar.Header{Name: entry, Mode: 0755, Size: int64(len(data))}
		if link {
			h.Typeflag = tar.TypeSymlink
			h.Linkname = "/etc/passwd"
			h.Size = 0
		}
		if e := tr.WriteHeader(h); e != nil {
			t.Fatal(e)
		}
		if !link {
			if _, e := tr.Write(data); e != nil {
				t.Fatal(e)
			}
		}
		if e := tr.Close(); e != nil {
			t.Fatal(e)
		}
		if e := gz.Close(); e != nil {
			t.Fatal(e)
		}
	}
	return b.Bytes()
}
func TestExtract(t *testing.T) {
	for _, zipFormat := range []bool{false, true} {
		for _, tc := range []struct {
			name, entry string
			link, ok    bool
		}{{"normal", "pkg/proto-watch", false, true}, {"traversal", "../proto-watch", false, false}, {"symlink", "pkg/proto-watch", true, false}} {
			t.Run(fmt.Sprintf("%t/%s", zipFormat, tc.name), func(t *testing.T) {
				dir := t.TempDir()
				archive := filepath.Join(dir, "archive")
				target := filepath.Join(dir, "binary")
				if err := os.WriteFile(archive, pack(t, tc.entry, []byte("binary"), zipFormat, tc.link), 0600); err != nil {
					t.Fatal(err)
				}
				err := extract(archive, target, "pkg/proto-watch", zipFormat)
				if (err == nil) != tc.ok {
					t.Fatalf("extract: %v", err)
				}
				if tc.ok {
					b, _ := os.ReadFile(target)
					if string(b) != "binary" {
						t.Fatal("bad binary")
					}
				}
			})
		}
	}
}
func TestReplaceRollback(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "current")
	backup := filepath.Join(dir, "backup")
	os.WriteFile(current, []byte("original"), 0755)
	if err := replace(current, filepath.Join(dir, "missing"), backup); err == nil {
		t.Fatal("expected failure")
	}
	b, err := os.ReadFile(current)
	if err != nil || string(b) != "original" {
		t.Fatalf("original not restored: %s %v", b, err)
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixture(t *testing.T, archive []byte) (*Manager, release, map[string][]byte) {
	t.Helper()
	m := New("v0.2.1", func() {})
	m.executable = filepath.Join(t.TempDir(), "proto-watch")
	if err := os.WriteFile(m.executable, []byte("original"), 0755); err != nil {
		t.Fatal(err)
	}
	name := archiveName("v0.2.2", m.status.Platform)
	base := "https://github.com/" + repository + "/releases/download/v0.2.2/"
	r := release{Tag: "v0.2.2", Assets: []Asset{{Name: name, URL: base + name, Size: int64(len(archive))}, {Name: name + ".sha256", URL: base + name + ".sha256"}}}
	meta, _ := json.Marshal(r)
	data := map[string][]byte{"https://api.github.com/repos/" + repository + "/releases/latest": meta, base + name: archive, base + name + ".sha256": []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(archive), name))}
	m.client = &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
		b, ok := data[req.URL.String()]
		if !ok {
			t.Errorf("unexpected URL %s", req.URL)
			return nil, fmt.Errorf("unexpected URL")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(b)), Header: make(http.Header)}, nil
	})}
	return m, r, data
}
func TestInstallChecksumFailure(t *testing.T) {
	m, r, data := fixture(t, []byte("corrupt archive"))
	data[r.Assets[1].URL] = []byte(strings.Repeat("0", 64) + "  " + r.Assets[0].Name)
	if err := m.install(r); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("expected checksum error: %v", err)
	}
	b, _ := os.ReadFile(m.executable)
	if string(b) != "original" {
		t.Fatal("original was changed")
	}
	files, _ := os.ReadDir(filepath.Dir(m.executable))
	if len(files) != 1 {
		t.Fatal("staging files leaked")
	}
}
func TestInstallInvalidBinary(t *testing.T) {
	platform := runtime.GOOS + "-" + runtime.GOARCH
	binary := "proto-watch"
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	m, r, _ := fixture(t, pack(t, "proto-watch_v0.2.2_"+platform+"/"+binary, []byte("not executable"), runtime.GOOS == "windows", false))
	if err := m.install(r); err == nil {
		t.Fatal("expected binary validation failure")
	}
	b, _ := os.ReadFile(m.executable)
	if string(b) != "original" {
		t.Fatal("original changed")
	}
}
func TestInstallAndConcurrency(t *testing.T) {
	executable, _ := os.Executable()
	binaryData, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	platform := runtime.GOOS + "-" + runtime.GOARCH
	binary := "proto-watch"
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	m, _, _ := fixture(t, pack(t, "proto-watch_v0.2.2_"+platform+"/"+binary, binaryData, runtime.GOOS == "windows", false))
	if err = m.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !m.Status().Available {
		t.Fatal("release not available")
	}
	if err = m.Start("v9.9.9"); err == nil {
		t.Fatal("unreviewed version accepted")
	}
	done := make(chan struct{})
	m.restart = func() { close(done) }
	var wg sync.WaitGroup
	accepted := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); accepted <- m.Start("v0.2.2") }()
	}
	wg.Wait()
	close(accepted)
	success := 0
	for err := range accepted {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("accepted %d installs", success)
	}
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatalf("install did not complete: %+v", m.Status())
	}
	installed, err := os.ReadFile(m.executable)
	if err != nil || !bytes.Equal(installed, binaryData) {
		t.Fatal("incorrect executable installed")
	}
	status := m.Status()
	backup, err := os.ReadFile(status.Backup)
	if err != nil || string(backup) != "original" {
		t.Fatal("original backup missing")
	}
	if status.Phase != "restarting" {
		t.Fatal(status)
	}
}
func TestReleaseRestrictions(t *testing.T) {
	for _, raw := range []string{"http://github.com/file", "https://evil.example/file", "https://github.com:8443/file", "https://user@github.com/file"} {
		if trustedURL(raw) == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	m, r, _ := fixture(t, []byte("data"))
	r.Assets[0].URL = "https://github.com/other/repository/releases/download/v0.2.2/" + r.Assets[0].Name
	if _, _, err := selectAssets(r, m.status.Platform); err == nil {
		t.Fatal("foreign repository accepted")
	}
	if _, _, err := selectAssets(release{Tag: "v0.2.2"}, m.status.Platform); err == nil {
		t.Fatal("missing package accepted")
	}
}
func TestCheckFailureAndLimits(t *testing.T) {
	m, _, data := fixture(t, []byte("data"))
	endpoint := "https://api.github.com/repos/" + repository + "/releases/latest"
	data[endpoint] = []byte(`{"tag_name":"v0.2.2","prerelease":true}`)
	if err := m.Check(context.Background()); err == nil {
		t.Fatal("prerelease accepted")
	}
	if m.Status().Available {
		t.Fatal("available after failure")
	}
	data[endpoint] = []byte(strings.Repeat("x", 128))
	var out bytes.Buffer
	if err := m.get(context.Background(), endpoint, &out, 64); err == nil {
		t.Fatal("oversized response accepted")
	}
}

// Windows also needs to permit renaming an executable while its process is live.
func TestReplaceRunningExecutable(t *testing.T) {
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	current := filepath.Join(dir, "running.exe")
	staged := filepath.Join(dir, "next.exe")
	backup := filepath.Join(dir, "backup.exe")
	if err = os.WriteFile(current, data, 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(staged, []byte("replacement"), 0755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, current, "--hold")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("child not ready: %q %v", line, err)
	}
	if err = replace(current, staged, backup); err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(backup)
	if err != nil || !bytes.Equal(old, data) {
		t.Fatal("running binary backup corrupt")
	}
	next, err := os.ReadFile(current)
	if err != nil || string(next) != "replacement" {
		t.Fatal("new binary missing")
	}
}
