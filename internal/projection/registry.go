package projection

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Registry stores Projection implementations keyed by normalized name.
type Registry struct {
	mu          sync.RWMutex
	projections map[string]Projection
}

// NewRegistry returns an empty projection registry.
func NewRegistry() *Registry {
	return &Registry{projections: map[string]Projection{}}
}

// Register adds a projection and rejects nil, blank, or duplicate names.
func (registry *Registry) Register(projection Projection) error {
	if projection == nil {
		return errors.New("projection is nil")
	}
	name := normalizeRegistryName(projection.Name())
	if name == "" {
		return errors.New("projection name is required")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, exists := registry.projections[name]; exists {
		return fmt.Errorf("projection %q already registered", name)
	}
	registry.projections[name] = projection
	return nil
}

// Get returns the projection registered for name.
func (registry *Registry) Get(name string) (Projection, bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	projection, ok := registry.projections[normalizeRegistryName(name)]
	return projection, ok
}

// List returns the registered projections sorted by normalized name.
func (registry *Registry) List() []Projection {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	names := make([]string, 0, len(registry.projections))
	for name := range registry.projections {
		names = append(names, name)
	}
	sort.Strings(names)
	projections := make([]Projection, 0, len(names))
	for _, name := range names {
		projections = append(projections, registry.projections[name])
	}
	return projections
}

// Names returns the projection names in deterministic registry order.
func (registry *Registry) Names() []string {
	projections := registry.List()
	names := make([]string, 0, len(projections))
	for _, projection := range projections {
		names = append(names, projection.Name())
	}
	return names
}

func normalizeRegistryName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
