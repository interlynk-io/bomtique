// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package regfetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Defaults for the shared HTTP client. See TASKS.md M14.7:
//
//   - 10 s connect / 30 s total request timeout.
//   - 1 MiB response-body cap enforced via io.LimitReader.
//   - Accept: application/json default header.
//   - User-Agent identifying bomtique plus a contact URL.
const (
	defaultTimeout   = 30 * time.Second
	defaultMaxBytes  = 1 << 20 // 1 MiB
	defaultUserAgent = "bomtique/0.1.0 (+https://github.com/interlynk-io/bomtique)"
)

// Client is the shared HTTP client used by every importer. Zero-value
// construction via NewClient() is the supported path; direct struct
// literals are for tests that need to inject an httptest.Server.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	MaxBytes  int64
}

// NewClient returns a Client with the standard timeouts, cap, and
// User-Agent. Callers override individual fields for tests.
func NewClient() *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: defaultTimeout},
		UserAgent: defaultUserAgent,
		MaxBytes:  defaultMaxBytes,
	}
}

// SetUserAgentVersion refreshes the User-Agent template with the
// supplied version string. Used by the CLI entry point so builds
// with -ldflags "-X main.version=..." get their version reflected in
// registry logs.
func (c *Client) SetUserAgentVersion(version string) {
	if version == "" {
		return
	}
	c.UserAgent = fmt.Sprintf("bomtique/%s (+https://github.com/interlynk-io/bomtique)", version)
}

// Response captures the outcome of a single Get call. Body is
// pre-read (respecting MaxBytes) so the caller can json.Unmarshal it
// without further plumbing.
type Response struct {
	Status  int
	Headers http.Header
	Body    []byte
	URL     string
}

// Get performs one HTTP GET with the client's shared timeout, User-
// Agent, and JSON Accept header. extraHeaders are applied after the
// defaults so callers can add auth tokens. The return value:
//
//   - Non-nil error wrapping ErrNetwork for transport failures, or
//     ErrResponseTooLarge when the body overruns MaxBytes.
//   - Otherwise a Response, regardless of status code. Callers map
//     404 / 403 / 5xx to their own error semantics.
func (c *Client) Get(ctx context.Context, url string, extraHeaders map[string]string) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: new request %s: %w", ErrNetwork, url, err)
	}
	req.Header.Set("Accept", "application/json")
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: GET %s: %w", ErrNetwork, url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	maxBytes := c.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}

	// LimitReader of maxBytes+1 lets us detect overflow by reading
	// one byte past the cap; io.ReadAll stops at the limit so the
	// short-read check is cheap.
	limited := io.LimitReader(resp.Body, maxBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("%w: read %s: %w", ErrNetwork, url, err)
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("%w: GET %s produced %d bytes (cap %d)", ErrResponseTooLarge, url, len(body), maxBytes)
	}

	return &Response{
		Status:  resp.StatusCode,
		Headers: resp.Header.Clone(),
		Body:    body,
		URL:     url,
	}, nil
}
