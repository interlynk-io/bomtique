// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package regfetch

import "errors"

// Structured error sentinels surfaced by Client and the Importer
// implementations. Callers use errors.Is to branch; the wrapped
// error carries the specific URL / status / underlying transport
// failure.
var (
	// ErrNetwork wraps every transport-level failure: DNS lookup
	// failure, TCP connect timeout, TLS handshake error, context
	// cancellation mid-request.
	ErrNetwork = errors.New("network error")

	// ErrNotFound is the canonical 404 — package or ref doesn't
	// exist.
	ErrNotFound = errors.New("not found")

	// ErrRateLimited is produced on 403 / 429 responses that
	// carry a rate-limit hint header.
	ErrRateLimited = errors.New("rate limited")

	// ErrUnsupportedRef is returned by Fetch when no registered
	// importer matches the input string.
	ErrUnsupportedRef = errors.New("no importer matches the supplied ref")

	// ErrResponseTooLarge signals a response body larger than the
	// client's MaxBytes cap. Cap defaults to 1 MiB; raising it
	// would require code changes (not a flag) since larger bodies
	// indicate an unusual registry response.
	ErrResponseTooLarge = errors.New("response body exceeded size cap")

	// ErrOffline is returned when the caller requested a fetch but
	// supplied --offline. Surfaced so mutate.Add / Update can fall
	// back to the skeleton path cleanly.
	ErrOffline = errors.New("regfetch disabled (--offline)")
)
