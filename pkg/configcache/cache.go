// Package configcache keeps an in-process snapshot of the configs table.
//
// Reads take a read lock on an immutable map; a reload builds a new map and swaps it in,
// so readers never see a half-built snapshot. With a few hundred rows a full reload is cheap.
// Values are stored unmasked: this cache serves internal consumers only, masking happens in
// models.ToConfigResponse for API output.
package configcache

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/automax/backend/internal/models"
	"gorm.io/datatypes"
)

// Lister loads configs from storage; repository.ConfigRepository satisfies it.
type Lister interface {
	List(ctx context.Context, filter *models.ConfigListFilter) ([]models.Config, int64, error)
}

// Cache is a snapshot of every config row, read for one environment (dev | staging | prod).
type Cache struct {
	lister Lister
	env    string

	mu    sync.RWMutex
	items map[string]models.Config
}

// New creates an empty cache that reads env's configs. Call Load to fill it.
func New(lister Lister, env string) *Cache {
	return &Cache{lister: lister, env: env, items: map[string]models.Config{}}
}

func cacheKey(env, key string) string { return env + "\x00" + key }

// Environment returns the environment the getters read.
func (c *Cache) Environment() string { return c.env }

// Load replaces the snapshot with the current contents of the configs table.
func (c *Cache) Load(ctx context.Context) error {
	cfgs, _, err := c.lister.List(ctx, &models.ConfigListFilter{}) // Limit 0 = no limit
	if err != nil {
		return err
	}
	m := make(map[string]models.Config, len(cfgs))
	for _, cfg := range cfgs {
		m[cacheKey(cfg.Environment, cfg.CacheKey())] = cfg
	}
	c.mu.Lock()
	c.items = m
	c.mu.Unlock()
	return nil
}

// StartRefresh reloads the snapshot every interval until ctx is cancelled, so changes
// made by other backend instances show up here too.
func (c *Cache) StartRefresh(ctx context.Context, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := c.Load(ctx); err != nil {
					log.Printf("[ConfigCache] periodic reload failed: %v", err)
				}
			}
		}
	}()
}

// get looks a config up by its internal identifier ("module.name", case-insensitive).
func (c *Cache) get(key string) (models.Config, bool) {
	key = strings.ToLower(strings.TrimSpace(key))
	c.mu.RLock()
	cfg, ok := c.items[cacheKey(c.env, key)]
	c.mu.RUnlock()
	return cfg, ok
}

// Value returns the raw (unmasked) JSON value for key in the current environment.
func (c *Cache) Value(key string) (datatypes.JSON, bool) {
	cfg, ok := c.get(key)
	if !ok {
		return nil, false
	}
	return append(datatypes.JSON(nil), cfg.Value...), true
}

func (c *Cache) String(key, def string) string {
	var v string
	if !c.decode(key, &v) {
		return def
	}
	return v
}

func (c *Cache) Bool(key string, def bool) bool {
	var v bool
	if !c.decode(key, &v) {
		return def
	}
	return v
}

func (c *Cache) Int(key string, def int) int {
	var v int
	if !c.decode(key, &v) {
		return def
	}
	return v
}

// decode unmarshals the cached value into out; false means missing or wrong type.
func (c *Cache) decode(key string, out any) bool {
	cfg, ok := c.get(key)
	if !ok {
		return false
	}
	return json.Unmarshal(cfg.Value, out) == nil
}

// Get returns the non-secret config identified by module and name in the current environment.
// Secret configs are reported as missing so their existence isn't revealed, and there is no
// default or env-var fallback: a missing config is simply not found.
func (c *Cache) Get(module, name string) (*models.ConfigResponse, bool) {
	cfg, ok := c.get(models.ConfigCacheKey(strings.TrimSpace(module), strings.TrimSpace(name)))
	if !ok || cfg.IsSecret {
		return nil, false
	}
	resp := models.ToConfigResponse(&cfg)
	return &resp, true
}

// List returns every non-secret config of the current environment, sorted by module then name.
func (c *Cache) List() []models.ConfigResponse {
	c.mu.RLock()
	out := make([]models.ConfigResponse, 0, len(c.items))
	for _, cfg := range c.items {
		if cfg.Environment != c.env || cfg.IsSecret {
			continue
		}
		cfg := cfg
		out = append(out, models.ToConfigResponse(&cfg))
	}
	c.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Module != out[j].Module {
			return out[i].Module < out[j].Module
		}
		return out[i].Name < out[j].Name
	})
	return out
}
