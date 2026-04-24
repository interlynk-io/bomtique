// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package regfetch

import (
	"context"
	"fmt"
	"sync"

	"github.com/interlynk-io/bomtique/internal/manifest"
)

// Importer describes one registry-metadata source — GitHub, npm, PyPI
// — capable of turning a user-supplied ref into a conforming
// manifest.Component. Implementations live in sibling files
// (github.go, npm.go, ...) and register themselves via Register().
type Importer interface {
	// Name is a short human-readable label (e.g. "github", "npm").
	// Surfaced in error messages and the `--online` failure path.
	Name() string

	// Matches returns true when the importer wants to handle the
	// supplied ref. Matching is cheap — string prefix / purl type
	// inspection only. Importers MUST NOT make network calls from
	// Matches.
	Matches(ref string) bool

	// Fetch performs the registry lookup and returns a
	// newly-constructed Component. Errors wrap one of the sentinels
	// from errors.go.
	Fetch(ctx context.Context, client *Client, ref string) (*manifest.Component, error)
}

// Registry holds the set of importers available to Fetch. A process-
// global registry lives in defaultRegistry; Register appends to it in
// package-init functions. Tests construct isolated registries via
// NewRegistry to avoid interference from production registrations.
type Registry struct {
	mu        sync.RWMutex
	importers []Importer
}

// NewRegistry returns an empty registry. Tests use it to keep their
// importer set isolated from the process-global one.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register appends the importer to the registry. Safe for concurrent
// use; typical callers invoke it once from package init.
func (r *Registry) Register(imp Importer) {
	if imp == nil {
		return
	}
	r.mu.Lock()
	r.importers = append(r.importers, imp)
	r.mu.Unlock()
}

// Fetch walks the registered importers in registration order and
// calls Fetch on the first one whose Matches returns true.
// ErrUnsupportedRef is returned when no importer matches.
func (r *Registry) Fetch(ctx context.Context, client *Client, ref string) (*manifest.Component, error) {
	imp := r.pick(ref)
	if imp == nil {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedRef, ref)
	}
	return imp.Fetch(ctx, client, ref)
}

// Match returns the first importer that matches the ref, or nil when
// none do. Used by `bomtique manifest add` to decide whether the
// default auto-fetch path applies.
func (r *Registry) Match(ref string) Importer {
	return r.pick(ref)
}

func (r *Registry) pick(ref string) Importer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, imp := range r.importers {
		if imp.Matches(ref) {
			return imp
		}
	}
	return nil
}

// Importers returns a snapshot of the registered importers, in
// registration order. Callers must not mutate the returned slice.
func (r *Registry) Importers() []Importer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Importer, len(r.importers))
	copy(out, r.importers)
	return out
}

// defaultRegistry is the process-global registry. Production importer
// packages (github, npm, ...) call Register from their init() funcs.
var defaultRegistry = NewRegistry()

// Register adds imp to the process-global registry.
func Register(imp Importer) { defaultRegistry.Register(imp) }

// Default returns the process-global registry. Consumers pass it to
// Fetch when they want the full importer set.
func Default() *Registry { return defaultRegistry }

// Fetch is the convenience entry point that dispatches through the
// process-global registry.
func Fetch(ctx context.Context, client *Client, ref string) (*manifest.Component, error) {
	return defaultRegistry.Fetch(ctx, client, ref)
}

// Match is the convenience entry point for the process-global
// registry.
func Match(ref string) Importer { return defaultRegistry.Match(ref) }
