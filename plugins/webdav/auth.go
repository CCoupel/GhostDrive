// Package webdav implements a GhostDrive storage backend that communicates
// with WebDAV servers using standard HTTP verbs (PROPFIND, PUT, GET, DELETE,
// MOVE, MKCOL).  Authentication supports HTTP Basic and Bearer Token.
package webdav

import (
	"crypto/tls"
	"encoding/base64"
	"net/http"
	"time"
)

// authKind identifies the HTTP authentication scheme used by the backend.
type authKind string

const (
	// authBasic uses RFC 7617 HTTP Basic authentication (username:password).
	authBasic authKind = "basic"
	// authBearer uses RFC 6750 Bearer token authentication.
	authBearer authKind = "bearer"
)

// authConfig holds the credentials and TLS settings for one backend instance.
// Fields are immutable after Connect; never log password or token.
type authConfig struct {
	kind          authKind
	username      string
	password      string
	token         string
	tlsSkipVerify bool
}

// newHTTPClient builds an *http.Client configured for the given authConfig.
//
// TLS: if cfg.tlsSkipVerify is true, the transport accepts any certificate
// (useful for self-signed certs on home NAS devices).
// Timeout: 30 s covers typical file transfers up to a few hundred MB on a
// home network; callers that transfer large files should rely on per-request
// contexts for cancellation rather than this global timeout.
func newHTTPClient(cfg authConfig) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	transport.TLSClientConfig.InsecureSkipVerify = cfg.tlsSkipVerify //nolint:gosec // intentional
	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}
}

// applyAuth injects the appropriate Authorization header into req.
// For authBasic it encodes "username:password" in base64.
// For authBearer it sets "Bearer <token>".
// The password and token values are never logged.
func applyAuth(req *http.Request, cfg authConfig) {
	switch cfg.kind {
	case authBearer:
		req.Header.Set("Authorization", "Bearer "+cfg.token)
	default: // authBasic and any unknown kind fall back to Basic
		encoded := base64.StdEncoding.EncodeToString([]byte(cfg.username + ":" + cfg.password))
		req.Header.Set("Authorization", "Basic "+encoded)
	}
}
