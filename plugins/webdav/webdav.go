package webdav

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/CCoupel/GhostDrive/plugins"
)

// Sentinel errors
var (
	ErrNotConnected = fmt.Errorf("webdav: backend not connected")
	ErrFileNotFound = fmt.Errorf("webdav: file not found")
)

// Backend implements plugins.StorageBackend for WebDAV servers.
type Backend struct {
	mu        sync.RWMutex
	baseURL   string
	username  string
	password  string
	client    *http.Client
	connected bool
}

// New creates an unconnected WebDAV backend.
func New() *Backend {
	return &Backend{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Name returns the plugin identifier.
func (b *Backend) Name() string { return "webdav" }

// Connect initialises the backend from config params.
// Expected params: "url", optionally "username" and "password".
func (b *Backend) Connect(cfg plugins.BackendConfig) error {
	rawURL, ok := cfg.Params["url"]
	if !ok || rawURL == "" {
		return fmt.Errorf("webdav: connect: missing 'url' param")
	}

	// Validate URL
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("webdav: connect: invalid url %q: %w", rawURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("webdav: connect: unsupported scheme %q", parsed.Scheme)
	}

	b.mu.Lock()
	b.baseURL = strings.TrimRight(rawURL, "/")
	b.username = cfg.Params["username"]
	b.password = cfg.Params["password"]
	b.connected = false
	b.mu.Unlock()

	// Probe the server with a PROPFIND on root
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err = b.propfind(ctx, "/", 0)
	if err != nil {
		return fmt.Errorf("webdav: connect: probe failed: %w", err)
	}

	b.mu.Lock()
	b.connected = true
	b.mu.Unlock()

	return nil
}

// Disconnect marks the backend as disconnected.
func (b *Backend) Disconnect() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.connected = false
	return nil
}

// IsConnected returns the current connection state.
func (b *Backend) IsConnected() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.connected
}

// Upload copies a local file to the remote path.
func (b *Backend) Upload(ctx context.Context, local, remote string, progress plugins.ProgressCallback) error {
	if !b.IsConnected() {
		return ErrNotConnected
	}

	f, err := os.Open(local)
	if err != nil {
		return fmt.Errorf("webdav: upload %s: open local: %w", local, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("webdav: upload %s: stat local: %w", local, err)
	}
	total := info.Size()

	var reader io.Reader = f
	if progress != nil {
		reader = &progressReader{r: f, total: total, callback: progress}
	}

	req, err := http.NewRequestWithContext(ctx, "PUT", b.remoteURL(remote), reader)
	if err != nil {
		return fmt.Errorf("webdav: upload %s: build request: %w", remote, err)
	}
	req.ContentLength = total
	b.setAuth(req)

	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("webdav: upload %s: %w", remote, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("webdav: upload %s: unexpected status %d", remote, resp.StatusCode)
	}

	return nil
}

// Download copies a remote file to the local path.
func (b *Backend) Download(ctx context.Context, remote, local string, progress plugins.ProgressCallback) error {
	if !b.IsConnected() {
		return ErrNotConnected
	}

	req, err := http.NewRequestWithContext(ctx, "GET", b.remoteURL(remote), nil)
	if err != nil {
		return fmt.Errorf("webdav: download %s: build request: %w", remote, err)
	}
	b.setAuth(req)

	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("webdav: download %s: %w", remote, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("webdav: download %s: %w", remote, ErrFileNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("webdav: download %s: unexpected status %d", remote, resp.StatusCode)
	}

	if err := os.MkdirAll(path.Dir(local), 0755); err != nil {
		return fmt.Errorf("webdav: download %s: create local dir: %w", local, err)
	}

	f, err := os.Create(local)
	if err != nil {
		return fmt.Errorf("webdav: download %s: create local file: %w", local, err)
	}
	defer f.Close()

	var writer io.Writer = f
	if progress != nil {
		total := resp.ContentLength
		writer = &progressWriter{w: f, total: total, callback: progress}
	}

	if _, err := io.Copy(writer, resp.Body); err != nil {
		return fmt.Errorf("webdav: download %s: copy: %w", remote, err)
	}

	return nil
}

// Delete removes a remote file or directory.
func (b *Backend) Delete(ctx context.Context, remote string) error {
	if !b.IsConnected() {
		return ErrNotConnected
	}

	req, err := http.NewRequestWithContext(ctx, "DELETE", b.remoteURL(remote), nil)
	if err != nil {
		return fmt.Errorf("webdav: delete %s: build request: %w", remote, err)
	}
	b.setAuth(req)

	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("webdav: delete %s: %w", remote, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("webdav: delete %s: %w", remote, ErrFileNotFound)
	}
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("webdav: delete %s: unexpected status %d", remote, resp.StatusCode)
	}

	return nil
}

// Move renames/moves a remote resource.
func (b *Backend) Move(ctx context.Context, oldPath, newPath string) error {
	if !b.IsConnected() {
		return ErrNotConnected
	}

	req, err := http.NewRequestWithContext(ctx, "MOVE", b.remoteURL(oldPath), nil)
	if err != nil {
		return fmt.Errorf("webdav: move %s -> %s: build request: %w", oldPath, newPath, err)
	}
	req.Header.Set("Destination", b.remoteURL(newPath))
	req.Header.Set("Overwrite", "T")
	b.setAuth(req)

	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("webdav: move %s -> %s: %w", oldPath, newPath, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("webdav: move %s -> %s: unexpected status %d", oldPath, newPath, resp.StatusCode)
	}

	return nil
}

// List returns files and directories at the given remote path.
func (b *Backend) List(ctx context.Context, remotePath string) ([]plugins.FileInfo, error) {
	if !b.IsConnected() {
		return nil, ErrNotConnected
	}

	responses, err := b.propfind(ctx, remotePath, 1)
	if err != nil {
		return nil, fmt.Errorf("webdav: list %s: %w", remotePath, err)
	}

	var result []plugins.FileInfo
	for _, r := range responses {
		href := r.Href
		// Skip the directory itself (first entry is the path itself)
		if path.Clean(href) == path.Clean("/"+strings.TrimLeft(remotePath, "/")) {
			continue
		}

		fi := plugins.FileInfo{
			Name:    path.Base(href),
			Path:    href,
			IsDir:   r.PropStat.Prop.ResourceType.Collection != nil,
			ModTime: parseDAVTime(r.PropStat.Prop.GetLastModified),
			ETag:    strings.Trim(r.PropStat.Prop.GetETag, `"`),
		}
		if !fi.IsDir {
			fi.Size = r.PropStat.Prop.GetContentLength
		}
		result = append(result, fi)
	}

	return result, nil
}

// Stat returns information about a single remote file.
func (b *Backend) Stat(ctx context.Context, remotePath string) (*plugins.FileInfo, error) {
	if !b.IsConnected() {
		return nil, ErrNotConnected
	}

	responses, err := b.propfind(ctx, remotePath, 0)
	if err != nil {
		return nil, fmt.Errorf("webdav: stat %s: %w", remotePath, err)
	}

	if len(responses) == 0 {
		return nil, fmt.Errorf("webdav: stat %s: %w", remotePath, ErrFileNotFound)
	}

	r := responses[0]
	fi := &plugins.FileInfo{
		Name:    path.Base(remotePath),
		Path:    r.Href,
		IsDir:   r.PropStat.Prop.ResourceType.Collection != nil,
		ModTime: parseDAVTime(r.PropStat.Prop.GetLastModified),
		ETag:    strings.Trim(r.PropStat.Prop.GetETag, `"`),
	}
	if !fi.IsDir {
		fi.Size = r.PropStat.Prop.GetContentLength
	}

	return fi, nil
}

// CreateDir creates a directory at the remote path (MKCOL).
func (b *Backend) CreateDir(ctx context.Context, remotePath string) error {
	if !b.IsConnected() {
		return ErrNotConnected
	}

	req, err := http.NewRequestWithContext(ctx, "MKCOL", b.remoteURL(remotePath), nil)
	if err != nil {
		return fmt.Errorf("webdav: mkdir %s: build request: %w", remotePath, err)
	}
	b.setAuth(req)

	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("webdav: mkdir %s: %w", remotePath, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusMethodNotAllowed {
		return fmt.Errorf("webdav: mkdir %s: unexpected status %d", remotePath, resp.StatusCode)
	}

	return nil
}

// Watch polls the remote path every 30 seconds and emits FileEvents on changes.
// WebDAV has no native push notification — polling is the V1 approach.
func (b *Backend) Watch(ctx context.Context, remotePath string) (<-chan plugins.FileEvent, error) {
	if !b.IsConnected() {
		return nil, ErrNotConnected
	}

	ch := make(chan plugins.FileEvent, 64)

	go func() {
		defer close(ch)

		previous := map[string]plugins.FileInfo{}

		snapshot := func() {
			files, err := b.List(ctx, remotePath)
			if err != nil {
				return
			}

			current := map[string]plugins.FileInfo{}
			for _, fi := range files {
				current[fi.Path] = fi
			}

			for p, fi := range current {
				if old, exists := previous[p]; !exists {
					select {
					case ch <- plugins.FileEvent{Type: plugins.FileEventCreated, Path: p, Timestamp: time.Now(), Source: "remote"}:
					case <-ctx.Done():
						return
					}
					_ = fi
				} else if !fi.ModTime.Equal(old.ModTime) || fi.Size != old.Size {
					select {
					case ch <- plugins.FileEvent{Type: plugins.FileEventModified, Path: p, Timestamp: time.Now(), Source: "remote"}:
					case <-ctx.Done():
						return
					}
				}
			}

			for p := range previous {
				if _, exists := current[p]; !exists {
					select {
					case ch <- plugins.FileEvent{Type: plugins.FileEventDeleted, Path: p, Timestamp: time.Now(), Source: "remote"}:
					case <-ctx.Done():
						return
					}
				}
			}

			previous = current
		}

		// Initial snapshot (no events)
		files, _ := b.List(ctx, remotePath)
		for _, fi := range files {
			previous[fi.Path] = fi
		}

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				snapshot()
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

// ─── Internal helpers ────────────────────────────────────────────────────────

func (b *Backend) remoteURL(remotePath string) string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if !strings.HasPrefix(remotePath, "/") {
		remotePath = "/" + remotePath
	}
	return b.baseURL + remotePath
}

func (b *Backend) setAuth(req *http.Request) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.username != "" {
		req.SetBasicAuth(b.username, b.password)
	}
}

// ─── WebDAV XML structures (RFC 4918) ────────────────────────────────────────

type davMultistatus struct {
	XMLName   xml.Name      `xml:"multistatus"`
	Responses []davResponse `xml:"response"`
}

type davResponse struct {
	Href     string       `xml:"href"`
	PropStat davPropStat  `xml:"propstat"`
}

type davPropStat struct {
	Prop   davProp `xml:"prop"`
	Status string  `xml:"status"`
}

type davProp struct {
	GetLastModified  string          `xml:"getlastmodified"`
	GetContentLength int64           `xml:"getcontentlength"`
	GetETag          string          `xml:"getetag"`
	ResourceType     davResourceType `xml:"resourcetype"`
}

type davResourceType struct {
	Collection *struct{} `xml:"collection"`
}

func (b *Backend) propfind(ctx context.Context, remotePath string, depth int) ([]davResponse, error) {
	body := `<?xml version="1.0" encoding="utf-8"?>
<propfind xmlns="DAV:">
  <prop>
    <getlastmodified/>
    <getcontentlength/>
    <getetag/>
    <resourcetype/>
  </prop>
</propfind>`

	req, err := http.NewRequestWithContext(ctx, "PROPFIND", b.remoteURL(remotePath), strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build PROPFIND request: %w", err)
	}
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.Header.Set("Depth", fmt.Sprintf("%d", depth))
	b.setAuth(req)

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("PROPFIND: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("PROPFIND %s: %w", remotePath, ErrFileNotFound)
	}
	if resp.StatusCode != http.StatusMultiStatus {
		return nil, fmt.Errorf("PROPFIND %s: unexpected status %d", remotePath, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("PROPFIND read body: %w", err)
	}

	var ms davMultistatus
	if err := xml.Unmarshal(data, &ms); err != nil {
		return nil, fmt.Errorf("PROPFIND parse XML: %w", err)
	}

	return ms.Responses, nil
}

func parseDAVTime(s string) time.Time {
	formats := []string{
		time.RFC1123,
		time.RFC1123Z,
		"Mon, 02 Jan 2006 15:04:05 GMT",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// ─── Progress helpers ────────────────────────────────────────────────────────

type progressReader struct {
	r        io.Reader
	total    int64
	done     int64
	callback plugins.ProgressCallback
}

func (pr *progressReader) Read(p []byte) (n int, err error) {
	n, err = pr.r.Read(p)
	pr.done += int64(n)
	if pr.callback != nil {
		pr.callback(pr.done, pr.total)
	}
	return
}

type progressWriter struct {
	w        io.Writer
	total    int64
	done     int64
	callback plugins.ProgressCallback
}

func (pw *progressWriter) Write(p []byte) (n int, err error) {
	n, err = pw.w.Write(p)
	pw.done += int64(n)
	if pw.callback != nil {
		pw.callback(pw.done, pw.total)
	}
	return
}
