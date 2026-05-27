package main

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"
)

// tenantSettings is the tenant-scoped trust-ceremony configuration: dry-run
// staging gate and the pause-all-agents kill switch. Both default off so
// upgrades are transparent.
type tenantSettings struct {
	TenantID      string `json:"tenant_id"`
	DryRunEnabled bool   `json:"dry_run_enabled"`
	AgentsPaused  bool   `json:"agents_paused"`
	UpdatedAt     string `json:"updated_at,omitempty"`
}

// tenantSettingsStore is the persistence surface for tenantSettings. Both the
// SQLite-backed boardStore and the in-memory test store satisfy it.
type tenantSettingsStore interface {
	LoadTenantSettings(ctx context.Context, tenantID string) (tenantSettings, error)
	SaveTenantSettings(ctx context.Context, settings tenantSettings) error
}

// ErrTenantSettingsNotFound is returned when no row exists for the requested
// tenant. Callers should treat this as "all defaults" rather than an error.
var ErrTenantSettingsNotFound = errors.New("tenant settings not found")

// Compile-time check.
var _ tenantSettingsStore = (*sqliteBoardStore)(nil)

// tenantSettingsCache is a thin in-memory cache layered over the store so that
// hot-path ApplyToolCall lookups do not slam SQLite on every action. It is
// invalidated whenever SaveTenantSettings runs through tenantSettingsManager.
type tenantSettingsCache struct {
	mu       sync.RWMutex
	settings map[string]tenantSettings
}

func newTenantSettingsCache() *tenantSettingsCache {
	return &tenantSettingsCache{settings: map[string]tenantSettings{}}
}

func (c *tenantSettingsCache) get(tenantID string) (tenantSettings, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	settings, ok := c.settings[normalizeTenantID(tenantID)]
	return settings, ok
}

func (c *tenantSettingsCache) put(settings tenantSettings) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.settings[normalizeTenantID(settings.TenantID)] = settings
}

// tenantSettingsManager combines the store with the cache. Production code
// constructs one of these per process; the package-level `tenantSettings`
// variable holds the singleton.
type tenantSettingsManager struct {
	store tenantSettingsStore
	cache *tenantSettingsCache
}

func newTenantSettingsManager(store tenantSettingsStore) *tenantSettingsManager {
	return &tenantSettingsManager{store: store, cache: newTenantSettingsCache()}
}

// Get returns the persisted tenantSettings, falling back to all-defaults if
// no row exists. The returned struct is always populated with a normalized
// tenant ID so callers can use it as a logging tag.
func (m *tenantSettingsManager) Get(ctx context.Context, tenantID string) tenantSettings {
	tenantID = normalizeTenantID(tenantID)
	if m == nil {
		return tenantSettings{TenantID: tenantID}
	}
	if cached, ok := m.cache.get(tenantID); ok {
		return cached
	}
	if m.store == nil {
		return tenantSettings{TenantID: tenantID}
	}
	settings, err := m.store.LoadTenantSettings(ctx, tenantID)
	if errors.Is(err, ErrTenantSettingsNotFound) || err != nil {
		settings = tenantSettings{TenantID: tenantID}
	}
	m.cache.put(settings)
	return settings
}

// Set persists the supplied tenantSettings and invalidates the cache. The
// returned struct includes the freshly-stamped UpdatedAt timestamp.
func (m *tenantSettingsManager) Set(ctx context.Context, settings tenantSettings) (tenantSettings, error) {
	settings.TenantID = normalizeTenantID(settings.TenantID)
	settings.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if m == nil || m.store == nil {
		// in-memory only path
		if m != nil {
			m.cache.put(settings)
		}
		return settings, nil
	}
	if err := m.store.SaveTenantSettings(ctx, settings); err != nil {
		return tenantSettings{}, err
	}
	m.cache.put(settings)
	return settings, nil
}

// DryRunEnabled is a hot-path helper used by ApplyToolCallWithMeta to decide
// whether to stage or apply.
func (m *tenantSettingsManager) DryRunEnabled(ctx context.Context, tenantID string) bool {
	return m.Get(ctx, tenantID).DryRunEnabled
}

// AgentsPaused is a hot-path helper used by RunCoordinator.Start gating.
func (m *tenantSettingsManager) AgentsPaused(ctx context.Context, tenantID string) bool {
	return m.Get(ctx, tenantID).AgentsPaused
}

// memoryTenantSettingsStore is the test-side store.
type memoryTenantSettingsStore struct {
	mu       sync.Mutex
	settings map[string]tenantSettings
}

func newMemoryTenantSettingsStore() *memoryTenantSettingsStore {
	return &memoryTenantSettingsStore{settings: map[string]tenantSettings{}}
}

func (s *memoryTenantSettingsStore) LoadTenantSettings(_ context.Context, tenantID string) (tenantSettings, error) {
	tenantID = normalizeTenantID(tenantID)
	s.mu.Lock()
	defer s.mu.Unlock()
	settings, ok := s.settings[tenantID]
	if !ok {
		return tenantSettings{}, ErrTenantSettingsNotFound
	}
	return settings, nil
}

func (s *memoryTenantSettingsStore) SaveTenantSettings(_ context.Context, settings tenantSettings) error {
	settings.TenantID = normalizeTenantID(settings.TenantID)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings[settings.TenantID] = settings
	return nil
}

// SQLite implementation

func (store *sqliteBoardStore) LoadTenantSettings(ctx context.Context, tenantID string) (tenantSettings, error) {
	tenantID = normalizeTenantID(tenantID)
	var (
		dryRun, paused int
		updatedAt      string
	)
	err := store.db.QueryRowContext(ctx, `
		SELECT dry_run_enabled, agents_paused, updated_at
		FROM tenant_settings
		WHERE tenant_id = ?
	`, tenantID).Scan(&dryRun, &paused, &updatedAt)
	if err == sql.ErrNoRows {
		return tenantSettings{}, ErrTenantSettingsNotFound
	}
	if err != nil {
		return tenantSettings{}, err
	}
	return tenantSettings{
		TenantID:      tenantID,
		DryRunEnabled: dryRun != 0,
		AgentsPaused:  paused != 0,
		UpdatedAt:     updatedAt,
	}, nil
}

func (store *sqliteBoardStore) SaveTenantSettings(ctx context.Context, settings tenantSettings) error {
	settings.TenantID = normalizeTenantID(settings.TenantID)
	if settings.UpdatedAt == "" {
		settings.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	_, err := store.db.ExecContext(ctx, `
		INSERT INTO tenant_settings(tenant_id, dry_run_enabled, agents_paused, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(tenant_id) DO UPDATE SET
			dry_run_enabled = excluded.dry_run_enabled,
			agents_paused = excluded.agents_paused,
			updated_at = excluded.updated_at
	`, settings.TenantID, boolToInt(settings.DryRunEnabled), boolToInt(settings.AgentsPaused), settings.UpdatedAt)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
