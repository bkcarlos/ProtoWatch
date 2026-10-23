// Package update installs verified stable releases from the fixed upstream repository.
package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const repository = "bkcarlos/ProtoWatch"
const maxArchive = 128 << 20
const maxBinary = 256 << 20

var stableVersion = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}
type release struct {
	Tag        string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}
type Status struct {
	Current    string `json:"current"`
	Platform   string `json:"platform"`
	Latest     string `json:"latest,omitempty"`
	URL        string `json:"url,omitempty"`
	Phase      string `json:"phase"`
	Available  bool   `json:"available"`
	Error      string `json:"error,omitempty"`
	Backup     string `json:"backup,omitempty"`
	Downloaded int64  `json:"downloaded"`
	Total      int64  `json:"total"`
}
type Manager struct {
	mu         sync.Mutex
	status     Status
	selected   release
	client     *http.Client
	executable string
	restart    func()
}

func New(current string, restart func()) *Manager {
	path, err := os.Executable()
	if err == nil {
		path, err = filepath.EvalSymlinks(path)
	}
	m := &Manager{status: Status{Current: current, Platform: runtime.GOOS + "-" + runtime.GOARCH, Phase: "idle"}, executable: path, restart: restart}
	if err != nil {
		m.status.Error = err.Error()
	}
	m.client = &http.Client{Timeout: 30 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return trustedURL(r.URL.String())
	}}
	return m
}
func trustedURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return errors.New("invalid release URL")
	}
	switch u.Hostname() {
	case "api.github.com", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return nil
	}
	return errors.New("untrusted release host")
}
func (m *Manager) Status() Status { m.mu.Lock(); defer m.mu.Unlock(); return m.status }
func active(phase string) bool {
	return phase == "checking" || phase == "downloading" || phase == "installing" || phase == "restarting"
}
func (m *Manager) setPhase(phase string) { m.mu.Lock(); defer m.mu.Unlock(); m.status.Phase = phase }
func (m *Manager) failed(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.Phase = "error"
	m.status.Error = err.Error()
}
func newer(latest, current string) bool {
	a := stableVersion.FindStringSubmatch(latest)
	b := stableVersion.FindStringSubmatch(strings.SplitN(strings.TrimPrefix(current, "v"), "-", 2)[0])
	if a == nil || b == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		x, e := strconv.ParseUint(a[i], 10, 64)
		y, f := strconv.ParseUint(b[i], 10, 64)
		if e != nil || f != nil {
			return false
		}
		if x != y {
			return x > y
		}
	}
	return strings.Contains(current, "-")
}
func archiveName(tag, platform string) string {
	ext := ".tar.gz"
	if strings.HasPrefix(platform, "windows-") {
		ext = ".zip"
	}
	return "proto-watch_" + tag + "_" + platform + ext
}
func selectAssets(r release, platform string) (Asset, Asset, error) {
	name := archiveName(r.Tag, platform)
	var archive, checksum Asset
	for _, a := range r.Assets {
		if a.Name == name {
			archive = a
		}
		if a.Name == name+".sha256" {
			checksum = a
		}
	}
	if archive.URL == "" || checksum.URL == "" || archive.Size <= 0 || archive.Size > maxArchive {
		return archive, checksum, errors.New("release has no supported package/checksum for this platform")
	}
	if err := trustedURL(archive.URL); err != nil {
		return archive, checksum, err
	}
	if err := trustedURL(checksum.URL); err != nil {
		return archive, checksum, err
	}
	prefix := "https://github.com/" + repository + "/releases/download/" + r.Tag + "/"
	if archive.URL != prefix+name || checksum.URL != prefix+name+".sha256" {
		return archive, checksum, errors.New("release assets must belong to the upstream release")
	}
	return archive, checksum, nil
}
func (m *Manager) get(ctx context.Context, raw string, dst io.Writer, limit int64) error {
	if err := trustedURL(raw); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ProtoWatch-Updater")
	req.Header.Set("Accept", "application/octet-stream")
	if strings.HasPrefix(raw, "https://api.github.com/") {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("GitHub returned HTTP %d", resp.StatusCode)
	}
	n, err := io.Copy(dst, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return errors.New("release response exceeds size limit")
	}
	return nil
}
func (m *Manager) Check(ctx context.Context) error {
	m.mu.Lock()
	if active(m.status.Phase) {
		m.mu.Unlock()
		return errors.New("update operation already running")
	}
	m.status.Phase = "checking"
	m.status.Error = ""
	m.status.Available = false
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var data strings.Builder
	err := m.get(ctx, "https://api.github.com/repos/"+repository+"/releases/latest", &data, 2<<20)
	var r release
	if err == nil {
		err = json.Unmarshal([]byte(data.String()), &r)
	}
	if err == nil && (r.Draft || r.Prerelease || !stableVersion.MatchString(r.Tag)) {
		err = errors.New("latest release is not a stable semantic version")
	}
	if err == nil && !stableVersion.MatchString(strings.SplitN(strings.TrimPrefix(m.status.Current, "v"), "-", 2)[0]) {
		err = errors.New("development build has no comparable version; install a numbered release first")
	}
	if err == nil {
		_, _, err = selectAssets(r, m.status.Platform)
	}
	if err != nil {
		m.failed(err)
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.selected = r
	m.status.Latest = r.Tag
	m.status.URL = "https://github.com/" + repository + "/releases/tag/" + r.Tag
	m.status.Available = newer(r.Tag, m.status.Current)
	m.status.Phase = "idle"
	return nil
}

// Start binds installation to the version the user reviewed, and returns immediately.
func (m *Manager) Start(version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if active(m.status.Phase) || !m.status.Available || version != m.selected.Tag || m.restart == nil {
		return errors.New("check for an available release before installing")
	}
	m.status.Phase = "downloading"
	m.status.Downloaded = 0
	archive, _, _ := selectAssets(m.selected, m.status.Platform)
	m.status.Total = archive.Size
	m.status.Error = ""
	r := m.selected
	go func() {
		if err := m.install(r); err != nil {
			m.failed(err)
			return
		}
		m.setPhase("restarting")
		m.restart()
	}()
	return nil
}
func (m *Manager) install(r release) error {
	archive, checksum, err := selectAssets(r, m.status.Platform)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	dir, err := os.MkdirTemp(filepath.Dir(m.executable), ".protowatch-update-")
	if err != nil {
		return fmt.Errorf("executable directory is not writable: %w", err)
	}
	defer os.RemoveAll(dir)
	var sum strings.Builder
	if err = m.get(ctx, checksum.URL, &sum, 4096); err != nil {
		return err
	}
	fields := strings.Fields(sum.String())
	if len(fields) != 2 || fields[1] != archive.Name {
		return errors.New("invalid release checksum file")
	}
	expected, err := hex.DecodeString(fields[0])
	if err != nil || len(expected) != sha256.Size {
		return errors.New("invalid SHA-256")
	}
	packagePath := filepath.Join(dir, "package")
	f, err := os.Create(packagePath)
	if err != nil {
		return err
	}
	hash := sha256.New()
	err = m.get(ctx, archive.URL, io.MultiWriter(f, hash, progressWriter{m}), maxArchive)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	info, err := os.Stat(packagePath)
	if err != nil {
		return err
	}
	if info.Size() != archive.Size || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), fields[0]) {
		return errors.New("release size or SHA-256 mismatch; original binary unchanged")
	}
	binary := "proto-watch"
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	target := filepath.Join(dir, binary)
	entry := "proto-watch_" + r.Tag + "_" + m.status.Platform + "/" + binary
	if err = extract(packagePath, target, entry, strings.HasSuffix(archive.Name, ".zip")); err != nil {
		return err
	}
	if err = os.Chmod(target, 0755); err != nil {
		return err
	}
	verifyCtx, verifyCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer verifyCancel()
	cmd := exec.CommandContext(verifyCtx, target, "--version")
	var output limitedOutput
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("new binary failed validation: %w", err)
	}
	if !strings.HasPrefix(output.String(), "proto-watch "+r.Tag+" (") {
		return errors.New("new binary version does not match release")
	}
	if err = preservePermissions(m.executable, target); err != nil {
		return err
	}
	m.setPhase("installing")
	backup := m.executable + ".backup-" + time.Now().UTC().Format("20060102T150405.000000000")
	if err = replace(m.executable, target, backup); err != nil {
		return err
	}
	m.mu.Lock()
	m.status.Backup = backup
	m.mu.Unlock()
	return nil
}

type limitedOutput struct{ strings.Builder }

func (b *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len() < 4096 {
		keep := 4096 - b.Len()
		if keep > len(p) {
			keep = len(p)
		}
		b.Builder.Write(p[:keep])
	}
	return n, nil
}
func replace(current, staged, backup string) error {
	if err := os.Rename(current, backup); err != nil {
		return fmt.Errorf("back up current binary: %w", err)
	}
	if err := os.Rename(staged, current); err != nil {
		if rollback := os.Rename(backup, current); rollback != nil {
			return fmt.Errorf("install failed: %v; restore %s manually: %w", err, backup, rollback)
		}
		return fmt.Errorf("install failed; original restored: %w", err)
	}
	return nil
}
func extract(archive, target, entry string, isZip bool) error {
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer out.Close()
	found := false
	copyEntry := func(r io.Reader, size int64) error {
		if found || size <= 0 || size > maxBinary {
			return errors.New("invalid or duplicate executable entry")
		}
		found = true
		n, e := io.Copy(out, io.LimitReader(r, maxBinary+1))
		if e != nil {
			return e
		}
		if n != size {
			return errors.New("invalid executable size")
		}
		return nil
	}
	if isZip {
		z, e := zip.OpenReader(archive)
		if e != nil {
			return e
		}
		defer z.Close()
		for _, f := range z.File {
			if f.Name != entry {
				continue
			}
			if !f.Mode().IsRegular() {
				return errors.New("executable entry is not a regular file")
			}
			r, e := f.Open()
			if e != nil {
				return e
			}
			e = copyEntry(r, int64(f.UncompressedSize64))
			r.Close()
			if e != nil {
				return e
			}
		}
	} else {
		f, e := os.Open(archive)
		if e != nil {
			return e
		}
		defer f.Close()
		gz, e := gzip.NewReader(f)
		if e != nil {
			return e
		}
		defer gz.Close()
		tr := tar.NewReader(io.LimitReader(gz, maxBinary+16<<20))
		for {
			h, e := tr.Next()
			if e == io.EOF {
				break
			}
			if e != nil {
				return e
			}
			if h.Name != entry {
				continue
			}
			if h.Typeflag != tar.TypeReg {
				return errors.New("executable entry is not a regular file")
			}
			if e = copyEntry(tr, h.Size); e != nil {
				return e
			}
		}
	}
	if !found {
		return errors.New("release does not contain the expected executable")
	}
	return out.Sync()
}

// Restart uses the original path: on Linux os.Executable changes after renaming the running file.
func (m *Manager) Restart() error { return restartProcess(m.executable) }

type progressWriter struct{ manager *Manager }

func (p progressWriter) Write(b []byte) (int, error) {
	p.manager.mu.Lock()
	p.manager.status.Downloaded += int64(len(b))
	p.manager.mu.Unlock()
	return len(b), nil
}
