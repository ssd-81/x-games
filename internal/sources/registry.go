// Package sources contains the adapters that fetch build-events from the
// sites that publish them. Every adapter implements domain.Source.
package sources

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/ssd-81/x-games/internal/domain"
)

// Registry holds the known sources.
type Registry struct {
	mu      sync.RWMutex
	sources []domain.Source
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{} }

// Register adds a source.
func (r *Registry) Register(s domain.Source) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sources = append(r.sources, s)
}

// All returns every registered source.
func (r *Registry) All() []domain.Source {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]domain.Source, len(r.sources))
	copy(out, r.sources)
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// Get returns the source with the given name.
func (r *Registry) Get(name string) (domain.Source, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.sources {
		if s.Name() == name {
			return s, true
		}
	}
	return nil, false
}

// Filter returns the sources matching the given names. An empty list returns
// all sources.
func (r *Registry) Filter(names []string) []domain.Source {
	if len(names) == 0 {
		return r.All()
	}
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	var out []domain.Source
	for _, s := range r.All() {
		if want[s.Name()] {
			out = append(out, s)
		}
	}
	return out
}

// Default returns a registry pre-populated with the built-in sources.
func Default(client HTTPClient) *Registry {
	r := NewRegistry()
	r.Register(&Devpost{Client: client})
	r.Register(&Unstop{Client: client})
	r.Register(&CTFtime{Client: client})
	return r
}

// HTTPClient is the subset of the HTTP client the adapters need.
type HTTPClient interface {
	GetJSON(ctx context.Context, url string, out any) error
	GetString(ctx context.Context, url string) (string, error)
	GetBytes(ctx context.Context, url string, headers map[string]string) ([]byte, error)
}

// ErrNotImplemented is returned by adapters that are not implemented yet.
var ErrNotImplemented = fmt.Errorf("source not implemented")
