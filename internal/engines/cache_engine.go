package engines

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/udbp/udbproxy/pkg/metrics"
	"github.com/udbp/udbproxy/pkg/types"
)

type CacheEntry struct {
	Data      interface{}
	ExpiresAt time.Time
	Database  string
	Table     string
	QueryHash string
}

type CachingEngine struct {
	BaseEngine
	config      *types.CacheConfig
	cache       map[string]*CacheEntry
	mu          sync.RWMutex
	redisClient interface{}
}

func NewCachingEngine(config *types.CacheConfig) *CachingEngine {
	engine := &CachingEngine{
		BaseEngine: BaseEngine{name: "caching"},
		config:     config,
		cache:      make(map[string]*CacheEntry),
	}

	if config != nil && config.Backend == "redis" {
		engine.initRedisClient()
	}

	return engine
}

func (e *CachingEngine) initRedisClient() {
}

func (e *CachingEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if e.config == nil || !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	if qc.Operation != types.OpSelect {
		return types.EngineResult{Continue: true}
	}

	cacheKey := e.generateCacheKey(qc)
	entry := e.getCacheEntry(cacheKey)

	if entry != nil {
		metrics.RecordCacheHit(ctx, true)

		if qc.Response == nil {
			qc.Response = &types.QueryResponse{}
		}

		if data, ok := entry.Data.([][]interface{}); ok {
			qc.Response.Data = data
			qc.Metadata["cache_hit"] = true
		}

		return types.EngineResult{
			Continue: true,
			Modified: true,
			Metadata: map[string]interface{}{"cache_hit": true},
		}
	}

	metrics.RecordCacheHit(ctx, false)
	qc.Metadata["cache_key"] = cacheKey

	return types.EngineResult{Continue: true}
}

func (e *CachingEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if e.config == nil || !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	if qc.Operation != types.OpSelect {
		return types.EngineResult{Continue: true}
	}

	cacheKeyI, ok := qc.Metadata["cache_key"].(string)
	if !ok || cacheKeyI == "" {
		return types.EngineResult{Continue: true}
	}

	if qc.Response == nil || qc.Response.Data == nil {
		return types.EngineResult{Continue: true}
	}

	e.setCacheEntry(cacheKeyI, &CacheEntry{
		Data:      qc.Response.Data,
		ExpiresAt: time.Now().Add(e.config.TTL),
		Database:  qc.Database,
		Table:     qc.Table,
		QueryHash: cacheKeyI,
	})

	return types.EngineResult{Continue: true}
}

func (e *CachingEngine) generateCacheKey(qc *types.QueryContext) string {
	if e.config.KeyPrefix != "" {
		prefix := e.config.KeyPrefix
		hash := e.hashQuery(qc.RawQuery)
		return fmt.Sprintf("%s:%s:%s", prefix, qc.Database, hash)
	}

	hash := e.hashQuery(qc.RawQuery)
	return fmt.Sprintf("query:%s:%s:%s", qc.Database, qc.Table, hash)
}

func (e *CachingEngine) hashQuery(query string) string {
	hasher := sha1.New()
	hasher.Write([]byte(query))
	return hex.EncodeToString(hasher.Sum(nil))
}

func (e *CachingEngine) getCacheEntry(key string) *CacheEntry {
	e.mu.RLock()
	defer e.mu.RUnlock()

	entry, exists := e.cache[key]
	if !exists {
		return nil
	}

	if time.Now().After(entry.ExpiresAt) {
		return nil
	}

	return entry
}

func (e *CachingEngine) setCacheEntry(key string, entry *CacheEntry) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if int64(len(e.cache)) >= e.config.MaxSize {
		e.evictOldest()
	}

	e.cache[key] = entry
}

func (e *CachingEngine) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for key, entry := range e.cache {
		if oldestTime.IsZero() || entry.ExpiresAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.ExpiresAt
		}
	}

	if oldestKey != "" {
		delete(e.cache, oldestKey)
	}
}

func (e *CachingEngine) InvalidateDatabase(database string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for key, entry := range e.cache {
		if entry.Database == database {
			delete(e.cache, key)
		}
	}
}

func (e *CachingEngine) InvalidateTable(database, table string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for key, entry := range e.cache {
		if entry.Database == database && entry.Table == table {
			delete(e.cache, key)
		}
	}
}

func (e *CachingEngine) Clear() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cache = make(map[string]*CacheEntry)
}

func (e *CachingEngine) GetStats() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return map[string]interface{}{
		"entries":  len(e.cache),
		"max_size": e.config.MaxSize,
		"ttl":      e.config.TTL.String(),
		"backend":  e.config.Backend,
		"enabled":  e.config.Enabled,
	}
}

func (e *CachingEngine) SetConfig(config *types.CacheConfig) {
	e.config = config
}

func (e *CachingEngine) SetEnabled(enabled bool) {
	if e.config != nil {
		e.config.Enabled = enabled
	}
}

func (e *CachingEngine) SetTTL(ttl time.Duration) {
	if e.config != nil {
		e.config.TTL = ttl
	}
}
