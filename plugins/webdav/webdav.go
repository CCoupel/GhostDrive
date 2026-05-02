package webdav

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CCoupel/GhostDrive/internal/logger"
	"github.com/CCoupel/GhostDrive/plugins"
)

// ─── Sentinel errors ─────────────────────────────────────────────────────────

var (
	// ErrNotConnected wraps the shared sentinel so callers can use errors.Is
	// against either this package's value or plugins.ErrNotConnected.
	ErrNotConnected = fmt.Errorf("webdav: %w", plugins.ErrNotConnected)
	// ErrFileNotFound wraps the shared sentinel.
	ErrFileNotFound = fmt.Errorf("webdav: %w", plugins.ErrFileNotFound)
)

// ─── PROPFIND request body ────────────────────────────────────────────────────

// propfindBody is the minimal WebDAV PROPFIND XML body sent for every
// directory listing and Stat query.
const propfindBody = `<?xml version="1.0" encoding="utf-8"?>` +
	`<D:propfind xmlns:D="DAV:">` +
	`<D:prop>` +
	`<D:resourcetype/>` +
	`<D:getcontentlength/>` +
	`<D:getlastmodified/>` +
	`<D:getetag/>` +
	`<D:quota-available-bytes/>` +
	`<D:quota-used-bytes/>` +
	`</D:prop>` +
	`</D:propfind>`

// propfindQuotaBody is a focused PROPFIND body that requests only the RFC 4331
// quota properties.  Used exclusively by GetQuota to avoid asking for
// irrelevant properties that some servers return in a different propstat block.
const propfindQuotaBody = `<?xml version="1.0" encoding="utf-8"?>` +
	`<D:propfind xmlns:D="DAV:"><D:prop>` +
	`<D:quota-available-bytes/>` +
	`<D:quota-used-bytes/>` +
	`</D:prop></D:propfind>`

// ─── XML response types ───────────────────────────────────────────────────────
// The DAV: namespace (with trailing colon) is the WebDAV standard namespace URI.
// Go's encoding/xml splits a tag "NS local" on the first space: NS="DAV:",
// local="multistatus" (etc.).  This matches responses that use either
// xmlns="DAV:" (default namespace) or xmlns:D="DAV:" (prefix style).

type multistatus struct {
	XMLName   xml.Name       `xml:"DAV: multistatus"`
	Responses []propResponse `xml:"DAV: response"`
}

type propResponse struct {
	XMLName  xml.Name    `xml:"DAV: response"`
	Href     string      `xml:"DAV: href"`
	Propstat []propstat  `xml:"DAV: propstat"`
}

type propstat struct {
	XMLName xml.Name `xml:"DAV: propstat"`
	Prop    davProp  `xml:"DAV: prop"`
	Status  string   `xml:"DAV: status"`
}

type davProp struct {
	XMLName        xml.Name     `xml:"DAV: prop"`
	ResourceType   resourceType `xml:"DAV: resourcetype"`
	ContentLength  string       `xml:"DAV: getcontentlength"`
	LastModified   string       `xml:"DAV: getlastmodified"`
	ETag           string       `xml:"DAV: getetag"`
	QuotaAvailable string       `xml:"DAV: quota-available-bytes"`
	QuotaUsed      string       `xml:"DAV: quota-used-bytes"`
}

// resourceType contains an optional <collection/> marker.
// Collection is non-nil when the entry is a directory.
type resourceType struct {
	Collection *struct{} `xml:"DAV: collection"`
}

// ─── Backend ──────────────────────────────────────────────────────────────────

// Backend is the WebDAV StorageBackend implementation.
// All exported methods are safe for concurrent use.
type Backend struct {
	mu        sync.RWMutex
	connected bool
	baseURL   string // scheme + host + path, no trailing slash (set in Connect)
	basePath  string // URL-path portion of baseURL, no trailing slash
	auth      authConfig
	client    *http.Client // immutable after Connect; nil when disconnected
	pollMs    int          // Watch polling interval in milliseconds (default 30)
	lastCfg   plugins.BackendConfig
}

// New returns an unconnected Backend.  Call Connect before any other method.
func New() *Backend { return &Backend{} }

// ─── Identification ───────────────────────────────────────────────────────────

// Name returns "webdav", the plugin type identifier used in BackendConfig.Type.
func (b *Backend) Name() string { return "webdav" }

// Describe implements plugins.StorageBackend.
// Returns the static descriptor used by the UI to build the Zone 2 form.
// Callable before Connect; performs no I/O.
func (b *Backend) Describe() plugins.PluginDescriptor {
	return plugins.PluginDescriptor{
		Type:        "webdav",
		DisplayName: "WebDAV",
		Description: "Synchronise via un serveur WebDAV (Nextcloud, ownCloud, NAS…)",
		Params: []plugins.ParamSpec{
			{Key: "url", Label: "URL serveur", Type: plugins.ParamTypeString, Required: true, Placeholder: "https://nas.local/dav"},
			{Key: "authType", Label: "Authentification", Type: plugins.ParamTypeSelect, Required: false, Default: "basic", Options: []string{"basic", "bearer"}},
			{Key: "username", Label: "Nom d'utilisateur", Type: plugins.ParamTypeString, Required: false, Placeholder: "admin"},
			{Key: "password", Label: "Mot de passe", Type: plugins.ParamTypePassword, Required: false},
			{Key: "token", Label: "Token Bearer", Type: plugins.ParamTypePassword, Required: false, HelpText: "Requis si authType=bearer"},
			{Key: "tlsSkipVerify", Label: "Ignorer erreurs TLS", Type: plugins.ParamTypeBool, Required: false, Default: "false", HelpText: "Accepter les certificats auto-signés"},
			{Key: "pollInterval", Label: "Intervalle Watch (ms)", Type: plugins.ParamTypeNumber, Required: false, Default: "30000"},
			{Key: "basePath", Label: "Chemin de base", Type: plugins.ParamTypePath, Required: false, Default: "/", Placeholder: "/dossier/sous-dossier", HelpText: "Chroot du backend sur le serveur distant (ex: /partage/documents)"},
		},
	}
}

// ─── Connection ───────────────────────────────────────────────────────────────

// Connect initialises the backend using cfg.
//
// Required Params:
//   - "url"      — WebDAV root URL, e.g. "https://nas.local/dav"
//   - "username" — required when authType is "basic" (default)
//   - "password" — optional for basic auth; never logged
//   - "token"    — required when authType is "bearer"
//
// Optional Params:
//   - "authType"      — "basic" (default) | "bearer"
//   - "pollInterval"  — Watch polling interval in milliseconds (default "30")
//   - "tlsSkipVerify" — "true" to accept self-signed certificates
//
// Connect probes the server with a PROPFIND depth=0 and returns an error if the
// server is unreachable, rejects credentials, or the URL is malformed.
// Calling Connect on an already-connected backend reconnects it.
func (b *Backend) Connect(cfg plugins.BackendConfig) error {
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}

	// ── Validate URL ──────────────────────────────────────────────────────────
	rawURL := strings.TrimRight(cfg.Params["url"], "/")
	if rawURL == "" {
		return fmt.Errorf("webdav: connect: 'url' param is required")
	}
	parsedURL, err := url.Parse(rawURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return fmt.Errorf("webdav: connect: invalid url %q", rawURL)
	}

	// ── Build auth config ─────────────────────────────────────────────────────
	kind := authKind(cfg.Params["authType"])
	if kind == "" {
		kind = authBasic
	}
	auth := authConfig{
		kind:     kind,
		username: cfg.Params["username"],
		password: cfg.Params["password"],
		token:    cfg.Params["token"],
	}
	if cfg.Params["tlsSkipVerify"] == "true" {
		auth.tlsSkipVerify = true
	}

	switch kind {
	case authBasic:
		if auth.username == "" {
			return fmt.Errorf("webdav: connect: 'username' required for basic auth")
		}
	case authBearer:
		if auth.token == "" {
			return fmt.Errorf("webdav: connect: 'token' required for bearer auth")
		}
	default:
		return fmt.Errorf("webdav: connect: unsupported authType %q (must be 'basic' or 'bearer')", kind)
	}

	// ── Parse optional params ─────────────────────────────────────────────────
	pollMs := 30_000
	if s := cfg.Params["pollInterval"]; s != "" {
		if n, atoiErr := strconv.Atoi(s); atoiErr == nil && n > 0 {
			pollMs = n
		}
	}

	// ── Build base URL and path ───────────────────────────────────────────────
	// cfg.Params["basePath"] takes priority over the legacy cfg.RemotePath field.
	cfgBasePath := strings.Trim(cfg.Params["basePath"], "/")
	if cfgBasePath == "" {
		cfgBasePath = strings.Trim(cfg.RemotePath, "/")
	}
	urlBasePath := strings.TrimRight(parsedURL.Path, "/")
	baseURL := rawURL
	if cfgBasePath != "" {
		for _, seg := range strings.Split(cfgBasePath, "/") {
			if seg != "" {
				urlBasePath += "/" + url.PathEscape(seg)
				baseURL += "/" + url.PathEscape(seg)
			}
		}
	}

	// ── Probe the server (PROPFIND depth=0 on root) ───────────────────────────
	client := newHTTPClient(auth)
	req, err := http.NewRequestWithContext(
		context.Background(), "PROPFIND", baseURL,
		strings.NewReader(propfindBody),
	)
	if err != nil {
		return fmt.Errorf("webdav: connect: build probe: %w", err)
	}
	req.Header.Set("Depth", "0")
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	applyAuth(req, auth)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webdav: connect: probe failed: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("webdav: connect: authentication failed (401)")
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("webdav: connect: probe returned HTTP %d", resp.StatusCode)
	}

	// ── Store state ───────────────────────────────────────────────────────────
	b.mu.Lock()
	defer b.mu.Unlock()
	b.connected = true
	b.baseURL = baseURL
	b.basePath = urlBasePath
	b.auth = auth
	b.client = client
	b.pollMs = pollMs
	b.lastCfg = cfg
	return nil
}

// Disconnect marks the backend as disconnected and releases the HTTP client.
// Safe to call on an already-disconnected backend (no-op).
func (b *Backend) Disconnect() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.connected = false
	b.client = nil
	return nil
}

// IsConnected returns true when Connect has succeeded and Disconnect has not
// been called since.  Thread-safe; does not perform I/O.
func (b *Backend) IsConnected() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.connected
}

// ─── Internal helpers ─────────────────────────────────────────────────────────

// remoteURL builds the full URL for a remote path relative to the backend root.
// Each path segment is percent-encoded so that filenames with spaces and
// special characters are transmitted correctly.
func (b *Backend) remoteURL(remotePath string) string {
	b.mu.RLock()
	baseURL := b.baseURL
	b.mu.RUnlock()

	remotePath = strings.TrimLeft(remotePath, "/")
	if remotePath == "" {
		return baseURL
	}
	segments := strings.Split(remotePath, "/")
	escaped := make([]string, len(segments))
	for i, seg := range segments {
		escaped[i] = url.PathEscape(seg)
	}
	return baseURL + "/" + strings.Join(escaped, "/")
}

// hrefToRelPath converts a PROPFIND response href (URL-encoded, absolute path)
// to a slash-separated path relative to the backend root.
func (b *Backend) hrefToRelPath(href string) string {
	b.mu.RLock()
	basePath := b.basePath
	b.mu.RUnlock()

	decoded, _ := url.PathUnescape(href)
	decoded = strings.TrimRight(decoded, "/")

	if basePath == "" {
		return strings.TrimLeft(decoded, "/")
	}
	if decoded == basePath {
		return ""
	}
	prefix := basePath + "/"
	if strings.HasPrefix(decoded, prefix) {
		return strings.TrimLeft(decoded[len(prefix):], "/")
	}
	// Fallback: strip leading slash only.
	return strings.TrimLeft(decoded, "/")
}

// do executes req after applying authentication.  On a 401 response from a
// Bearer-auth backend it attempts a one-time reconnect (token refresh) and
// retries, provided req.GetBody is set (i.e. the body can be replayed).
// Callers receive the raw *http.Response and must close its Body.
func (b *Backend) do(req *http.Request) (*http.Response, error) {
	b.mu.RLock()
	connected := b.connected
	client := b.client
	auth := b.auth
	b.mu.RUnlock()

	if !connected || client == nil {
		return nil, ErrNotConnected
	}

	applyAuth(req, auth)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("webdav: %s %s: %w", req.Method, req.URL.Path, err)
	}
	log.Printf("[webdav] %s %s → %d", req.Method, req.URL.Path, resp.StatusCode)

	// Retry once on 401 for Bearer (token may have expired).
	if resp.StatusCode == http.StatusUnauthorized &&
		auth.kind == authBearer &&
		req.GetBody != nil {

		resp.Body.Close()

		b.mu.RLock()
		lastCfg := b.lastCfg
		b.mu.RUnlock()

		_ = b.Connect(lastCfg) // best-effort token refresh; ignore error

		// Restore request body for replay.
		if newBody, gbErr := req.GetBody(); gbErr == nil {
			req.Body = newBody
		}

		b.mu.RLock()
		retryClient := b.client
		retryAuth := b.auth
		b.mu.RUnlock()

		if retryClient == nil {
			return nil, ErrNotConnected
		}
		applyAuth(req, retryAuth)
		resp2, err2 := retryClient.Do(req)
		if err2 != nil {
			return nil, fmt.Errorf("webdav: retry %s %s: %w", req.Method, req.URL.Path, err2)
		}
		log.Printf("[webdav] %s %s → %d (retry)", req.Method, req.URL.Path, resp2.StatusCode)
		return resp2, nil
	}

	return resp, nil
}

// propfind issues a PROPFIND request at targetURL with the given Depth header
// and returns the parsed multistatus response.
// Returns ErrFileNotFound (wrapped) when the server returns 404.
func (b *Backend) propfind(ctx context.Context, targetURL, depth string) (*multistatus, error) {
	body := propfindBody
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", targetURL, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("webdav: propfind: build request: %w", err)
	}
	req.Header.Set("Depth", depth)
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	// Provide GetBody so that do() can replay the body on a 401 Bearer retry.
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(body)), nil
	}

	resp, err := b.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrFileNotFound
	}
	if resp.StatusCode != http.StatusMultiStatus {
		return nil, fmt.Errorf("webdav: propfind: unexpected status %d", resp.StatusCode)
	}

	return parsePropfind(resp.Body)
}

// parsePropfind decodes a WebDAV 207 Multi-Status XML body.
func parsePropfind(body io.Reader) (*multistatus, error) {
	var ms multistatus
	if err := xml.NewDecoder(body).Decode(&ms); err != nil {
		return nil, fmt.Errorf("webdav: parse propfind: %w", err)
	}
	return &ms, nil
}

// propfindQuota issues a PROPFIND with the quota-only body at targetURL and
// returns the parsed multistatus.  Unlike propfind it reads the full body into
// memory first so it can be logged at DEBUG level for troubleshooting.
func (b *Backend) propfindQuota(ctx context.Context, targetURL string) (*multistatus, error) {
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", targetURL, strings.NewReader(propfindQuotaBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.Header.Set("Depth", "0")

	resp, err := b.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMultiStatus {
		return nil, fmt.Errorf("webdav: propfind quota: unexpected status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	logger.Debug("[webdav] propfind quota raw: %s", string(data))

	var ms multistatus
	if err := xml.Unmarshal(data, &ms); err != nil {
		return nil, err
	}
	return &ms, nil
}

// fileInfoFromResponse extracts plugins.FileInfo from a single propfind
// response entry.  remotePath is the slash-separated path relative to the
// backend root (already computed by the caller via hrefToRelPath).
func fileInfoFromResponse(resp propResponse, remotePath string) plugins.FileInfo {
	name := path.Base(remotePath)
	if name == "." || name == "" {
		name = "/"
	}

	fi := plugins.FileInfo{
		Name: name,
		Path: remotePath,
	}

	for _, ps := range resp.Propstat {
		// Only extract from the 200 OK propstat; skip 404/403 partial results.
		if !strings.Contains(ps.Status, "200") {
			continue
		}
		p := ps.Prop
		fi.IsDir = p.ResourceType.Collection != nil
		if p.ContentLength != "" {
			fi.Size, _ = strconv.ParseInt(p.ContentLength, 10, 64)
		}
		if p.LastModified != "" {
			fi.ModTime, _ = http.ParseTime(p.LastModified)
		}
		fi.ETag = strings.Trim(p.ETag, `"`)
	}

	return fi
}

// ─── File operations ──────────────────────────────────────────────────────────

// Upload copies the local file at local to the remote path remote using
// HTTP PUT.  The Content-Length header is set from the file size so that the
// server can show accurate upload progress.  Bearer retry is not supported for
// PUT (body is a streaming reader that cannot be replayed without re-opening
// the file); call Connect manually to refresh the token if needed.
func (b *Backend) Upload(ctx context.Context, local, remote string, progress plugins.ProgressCallback) error {
	if !b.IsConnected() {
		return ErrNotConnected
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("webdav: upload %s: %w", remote, err)
	}

	f, err := os.Open(local)
	if err != nil {
		return fmt.Errorf("webdav: upload %s: open: %w", remote, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("webdav: upload %s: stat: %w", remote, err)
	}
	totalSize := info.Size()

	var body io.Reader = f
	if progress != nil {
		body = &progressReader{r: f, total: totalSize, callback: progress}
	}

	req, err := http.NewRequestWithContext(ctx, "PUT", b.remoteURL(remote), body)
	if err != nil {
		return fmt.Errorf("webdav: upload %s: build request: %w", remote, err)
	}
	req.ContentLength = totalSize
	// GetBody intentionally NOT set: streaming body cannot be replayed cheaply.

	resp, err := b.do(req)
	if err != nil {
		return fmt.Errorf("webdav: upload %s: %w", remote, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusCreated &&
		resp.StatusCode != http.StatusNoContent &&
		resp.StatusCode != http.StatusOK {
		return fmt.Errorf("webdav: upload %s: server returned %d", remote, resp.StatusCode)
	}
	return nil
}

// Download fetches the remote file at remote and writes it to local.
// The parent directory of local is created if it does not exist.
// Returns ErrFileNotFound (wrapped) when remote does not exist.
func (b *Backend) Download(ctx context.Context, remote, local string, progress plugins.ProgressCallback) error {
	if !b.IsConnected() {
		return ErrNotConnected
	}

	req, err := http.NewRequestWithContext(ctx, "GET", b.remoteURL(remote), nil)
	if err != nil {
		return fmt.Errorf("webdav: download %s: build request: %w", remote, err)
	}

	resp, err := b.do(req)
	if err != nil {
		return fmt.Errorf("webdav: download %s: %w", remote, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotFound:
		return fmt.Errorf("webdav: download %s: %w", remote, ErrFileNotFound)
	case http.StatusOK:
		// continue
	default:
		return fmt.Errorf("webdav: download %s: server returned %d", remote, resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(local), 0755); err != nil {
		return fmt.Errorf("webdav: download %s: create parent dir: %w", local, err)
	}

	out, err := os.Create(local)
	if err != nil {
		return fmt.Errorf("webdav: download %s: create local file: %w", local, err)
	}
	defer out.Close()

	total := resp.ContentLength // -1 when unknown
	var writer io.Writer = out
	if progress != nil {
		writer = &progressWriter{w: out, total: total, callback: progress}
	}

	if _, err := io.Copy(writer, resp.Body); err != nil {
		return fmt.Errorf("webdav: download %s: write: %w", remote, err)
	}
	return nil
}

// Delete removes the file or directory at remote via HTTP DELETE.
// Returns ErrFileNotFound (wrapped) when remote does not exist.
func (b *Backend) Delete(ctx context.Context, remote string) error {
	if !b.IsConnected() {
		return ErrNotConnected
	}

	req, err := http.NewRequestWithContext(ctx, "DELETE", b.remoteURL(remote), nil)
	if err != nil {
		return fmt.Errorf("webdav: delete %s: build request: %w", remote, err)
	}

	resp, err := b.do(req)
	if err != nil {
		return fmt.Errorf("webdav: delete %s: %w", remote, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	switch resp.StatusCode {
	case http.StatusNotFound:
		return fmt.Errorf("webdav: delete %s: %w", remote, ErrFileNotFound)
	case http.StatusNoContent, http.StatusOK:
		return nil
	default:
		return fmt.Errorf("webdav: delete %s: server returned %d", remote, resp.StatusCode)
	}
}

// Move renames or moves the entry at oldPath to newPath via HTTP MOVE.
// Returns ErrFileNotFound (wrapped) when oldPath does not exist.
func (b *Backend) Move(ctx context.Context, oldPath, newPath string) error {
	if !b.IsConnected() {
		return ErrNotConnected
	}

	req, err := http.NewRequestWithContext(ctx, "MOVE", b.remoteURL(oldPath), nil)
	if err != nil {
		return fmt.Errorf("webdav: move %s → %s: build request: %w", oldPath, newPath, err)
	}
	req.Header.Set("Destination", b.remoteURL(newPath))
	req.Header.Set("Overwrite", "T")

	resp, err := b.do(req)
	if err != nil {
		return fmt.Errorf("webdav: move %s → %s: %w", oldPath, newPath, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	switch resp.StatusCode {
	case http.StatusNotFound, http.StatusForbidden:
		// RFC 4918 §9.9: 404 = source not found.
		// Some servers (including golang.org/x/net/webdav MemFS) return 403
		// when the source does not exist — treat both as ErrFileNotFound.
		return fmt.Errorf("webdav: move %s: %w", oldPath, ErrFileNotFound)
	case http.StatusCreated, http.StatusNoContent:
		return nil
	default:
		return fmt.Errorf("webdav: move %s → %s: server returned %d", oldPath, newPath, resp.StatusCode)
	}
}

// ─── Navigation ───────────────────────────────────────────────────────────────

// List returns the direct children of the directory at path.
// Returns an empty (non-nil) slice when the directory is empty.
// Returns ErrFileNotFound (wrapped) when path does not exist.
func (b *Backend) List(ctx context.Context, dirPath string) ([]plugins.FileInfo, error) {
	if !b.IsConnected() {
		return nil, ErrNotConnected
	}

	ms, err := b.propfind(ctx, b.remoteURL(dirPath), "1")
	if err != nil {
		return nil, fmt.Errorf("webdav: list %s: %w", dirPath, err)
	}

	requestedRel := strings.TrimLeft(dirPath, "/")
	result := make([]plugins.FileInfo, 0, len(ms.Responses))
	for _, resp := range ms.Responses {
		relPath := b.hrefToRelPath(resp.Href)
		// Skip the self-entry (the directory we listed).
		if relPath == requestedRel {
			continue
		}
		result = append(result, fileInfoFromResponse(resp, relPath))
	}
	return result, nil
}

// Stat returns metadata for the file or directory at filePath.
// Returns ErrFileNotFound (wrapped) when filePath does not exist.
func (b *Backend) Stat(ctx context.Context, filePath string) (*plugins.FileInfo, error) {
	if !b.IsConnected() {
		return nil, ErrNotConnected
	}

	ms, err := b.propfind(ctx, b.remoteURL(filePath), "0")
	if err != nil {
		return nil, fmt.Errorf("webdav: stat %s: %w", filePath, err)
	}

	if len(ms.Responses) == 0 {
		return nil, fmt.Errorf("webdav: stat %s: %w", filePath, ErrFileNotFound)
	}

	relPath := b.hrefToRelPath(ms.Responses[0].Href)
	// Preserve the caller's path if href-to-rel yielded empty (root) but caller
	// asked for a specific path — keep the caller-supplied path for Name/Path.
	if relPath == "" && strings.TrimLeft(filePath, "/") != "" {
		relPath = strings.TrimLeft(filePath, "/")
	}
	fi := fileInfoFromResponse(ms.Responses[0], relPath)
	return &fi, nil
}

// CreateDir creates the directory at dirPath via HTTP MKCOL.
// If the directory already exists (405 Method Not Allowed), the call is a no-op.
func (b *Backend) CreateDir(ctx context.Context, dirPath string) error {
	if !b.IsConnected() {
		return ErrNotConnected
	}

	req, err := http.NewRequestWithContext(ctx, "MKCOL", b.remoteURL(dirPath), nil)
	if err != nil {
		return fmt.Errorf("webdav: createDir %s: build request: %w", dirPath, err)
	}

	resp, err := b.do(req)
	if err != nil {
		return fmt.Errorf("webdav: createDir %s: %w", dirPath, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	switch resp.StatusCode {
	case http.StatusCreated:
		return nil
	case http.StatusMethodNotAllowed:
		// 405 = resource already exists — no-op per interface contract.
		return nil
	default:
		return fmt.Errorf("webdav: createDir %s: server returned %d", dirPath, resp.StatusCode)
	}
}

// ─── Watch ────────────────────────────────────────────────────────────────────

// Watch polls path every pollMs milliseconds and emits FileEvents on the
// returned channel when files are created, modified, or deleted.
// The channel (buffered, size 64) is closed when ctx is cancelled.
// Pre-condition: IsConnected() == true, else returns nil, ErrNotConnected.
func (b *Backend) Watch(ctx context.Context, watchPath string) (<-chan plugins.FileEvent, error) {
	if !b.IsConnected() {
		return nil, ErrNotConnected
	}

	b.mu.RLock()
	pollMs := b.pollMs
	b.mu.RUnlock()

	ch := make(chan plugins.FileEvent, 64)

	go func() {
		defer close(ch)

		ticker := time.NewTicker(time.Duration(pollMs) * time.Millisecond)
		defer ticker.Stop()

		// Establish the initial snapshot before starting the loop so that
		// files that already exist at Watch time are not emitted as Created.
		snapshot := buildSnapshot(ctx, b, watchPath)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !b.IsConnected() {
					return
				}

				current, err := b.List(ctx, watchPath)
				if err != nil {
					continue // transient error — retry on next tick
				}

				currentMap := make(map[string]plugins.FileInfo, len(current))
				for _, fi := range current {
					currentMap[fi.Path] = fi
				}

				// Detect created and modified files.
				for p, fi := range currentMap {
					old, exists := snapshot[p]
					var evType plugins.FileEventType
					if !exists {
						evType = plugins.FileEventCreated
					} else if fi.ModTime != old.ModTime || fi.Size != old.Size {
						evType = plugins.FileEventModified
					}
					if evType != "" {
						select {
						case ch <- plugins.FileEvent{
							Type:      evType,
							Path:      p,
							Timestamp: time.Now(),
							Source:    "remote",
						}:
						case <-ctx.Done():
							return
						}
					}
				}

				// Detect deleted files.
				for p := range snapshot {
					if _, exists := currentMap[p]; !exists {
						select {
						case ch <- plugins.FileEvent{
							Type:      plugins.FileEventDeleted,
							Path:      p,
							Timestamp: time.Now(),
							Source:    "remote",
						}:
						case <-ctx.Done():
							return
						}
					}
				}

				snapshot = currentMap
			}
		}
	}()

	return ch, nil
}

// buildSnapshot creates the initial path→FileInfo map for Watch.
// Errors are silently swallowed; the goroutine will retry on the first tick.
func buildSnapshot(ctx context.Context, b *Backend, watchPath string) map[string]plugins.FileInfo {
	entries, err := b.List(ctx, watchPath)
	if err != nil {
		return map[string]plugins.FileInfo{}
	}
	m := make(map[string]plugins.FileInfo, len(entries))
	for _, fi := range entries {
		m[fi.Path] = fi
	}
	return m
}

// ─── Quota ────────────────────────────────────────────────────────────────────

// GetQuota returns the free and total space reported by the server via
// RFC 4331 quota properties.  When the server does not support quota
// (properties absent or empty), (-1, -1, nil) is returned per the interface
// contract.
func (b *Backend) GetQuota(ctx context.Context) (free, total int64, err error) {
	if !b.IsConnected() {
		return 0, 0, ErrNotConnected
	}

	b.mu.RLock()
	baseURL := b.baseURL
	b.mu.RUnlock()

	ms, quotaErr := b.propfindQuota(ctx, baseURL)
	if quotaErr != nil {
		return -1, -1, nil // quota not available → degrade gracefully
	}
	if len(ms.Responses) == 0 {
		return -1, -1, nil
	}

	// Iterate over ALL propstat blocks: some servers (e.g. Nextcloud) return
	// quota properties in a non-200 propstat (e.g. 200 for available, separate
	// block for used), so checking only "200" blocks misses quota data.
	for _, ps := range ms.Responses[0].Propstat {
		p := ps.Prop
		if p.QuotaAvailable == "" && p.QuotaUsed == "" {
			continue
		}
		avail, aErr := strconv.ParseInt(strings.TrimSpace(p.QuotaAvailable), 10, 64)
		used, uErr := strconv.ParseInt(strings.TrimSpace(p.QuotaUsed), 10, 64)
		if aErr != nil || uErr != nil {
			continue
		}
		return avail, avail + used, nil
	}
	return -1, -1, nil
}

// ─── Progress helpers ─────────────────────────────────────────────────────────

// progressReader wraps an io.Reader and fires a ProgressCallback after each
// Read, reporting bytes done so far against the total file size.
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

// progressWriter wraps an io.Writer and fires a ProgressCallback after each
// Write, reporting bytes written so far against the expected total.
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
