package engines

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/udbp/udbproxy/pkg/types"
)

// QueryVersioningEngine tracks and compares query changes over time
type QueryVersioningEngine struct {
	BaseEngine
	config      *VersioningConfig
	versions    map[string]*QueryVersion
	currentVer  int64
	stats       *VersioningStats
	mu          sync.RWMutex
}

type VersioningConfig struct {
	Enabled        bool
	MaxVersions    int
	RetentionDays  int
	TrackOnly      bool // Track without blocking
	AutoTagChanges bool
}

type QueryVersion struct {
	QueryID       string
	Query         string
	Version       int64
	Timestamp     time.Time
	User          string
	Database      string
	Hash          string
	Changes       string // Description of changes from previous version
	Metadata      map[string]interface{}
}

type VersioningStats struct {
	VersionsStored  int64
	VersionsCompared int64
	DuplicateQueries int64
	AvgVersionLen   int
	mu              sync.RWMutex
}

// NewQueryVersioningEngine creates a new Query Versioning Engine
func NewQueryVersioningEngine(config *VersioningConfig) *QueryVersioningEngine {
	if config == nil {
		config = &VersioningConfig{
			Enabled:       false,
			MaxVersions:   1000,
			RetentionDays: 30,
		}
	}

	engine := &QueryVersioningEngine{
		BaseEngine: BaseEngine{name: "query_versioning"},
		config:     config,
		versions:   make(map[string]*QueryVersion),
		currentVer: 1,
		stats:      &VersioningStats{},
	}

	return engine
}

// Process tracks query versions
func (e *QueryVersioningEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	query := qc.RawQuery
	if query == "" {
		return types.EngineResult{Continue: true}
	}

	hash := e.hashQuery(query)

	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if query already exists
	if existing, ok := e.versions[hash]; ok {
		e.stats.mu.Lock()
		e.stats.DuplicateQueries++
		e.stats.mu.Unlock()

		if qc.Metadata == nil {
			qc.Metadata = make(map[string]interface{})
		}
		qc.Metadata["query_version"] = existing.Version
		qc.Metadata["query_exists"] = true

		return types.EngineResult{Continue: true}
	}

	// Create new version
	version := &QueryVersion{
		QueryID:   qc.ID,
		Query:     query,
		Version:   e.currentVer,
		Timestamp: time.Now(),
		User:      qc.User,
		Database:  qc.Database,
		Hash:      hash,
		Metadata:  qc.Metadata,
	}

	e.versions[hash] = version
	e.currentVer++

	e.stats.mu.Lock()
	e.stats.VersionsStored++
	e.stats.AvgVersionLen = (e.stats.AvgVersionLen*int(e.stats.VersionsStored-1) + len(query)) / int(e.stats.VersionsStored)
	e.stats.mu.Unlock()

	if qc.Metadata == nil {
		qc.Metadata = make(map[string]interface{})
	}
	qc.Metadata["query_version"] = version.Version
	qc.Metadata["query_new"] = true

	return types.EngineResult{Continue: true}
}

// ProcessResponse processes versioning response
func (e *QueryVersioningEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	return types.EngineResult{Continue: true}
}

// hashQuery creates a hash for query
func (e *QueryVersioningEngine) hashQuery(query string) string {
	hash := 0
	for _, c := range []byte(query) {
		hash = hash*31 + int(c)
	}
	return fmt.Sprintf("%x", hash)
}

// CompareVersions compares two query versions
func (e *QueryVersioningEngine) CompareVersions(hash1, hash2 string) (string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	v1, ok1 := e.versions[hash1]
	v2, ok2 := e.versions[hash2]

	if !ok1 || !ok2 {
		return "", fmt.Errorf("version not found")
	}

	e.stats.mu.Lock()
	e.stats.VersionsCompared++
	e.stats.mu.Unlock()

	return fmt.Sprintf("Version %d vs %d: %d chars vs %d chars", 
		v1.Version, v2.Version, len(v1.Query), len(v2.Query)), nil
}

// GetVersionStats returns versioning statistics
func (e *QueryVersioningEngine) GetVersionStats() VersioningStatsResponse {
	e.stats.mu.RLock()
	defer e.stats.mu.RUnlock()

	e.mu.RLock()
	versionCount := len(e.versions)
	e.mu.RUnlock()

	return VersioningStatsResponse{
		VersionsStored:     e.stats.VersionsStored,
		VersionsCompared:   e.stats.VersionsCompared,
		DuplicateQueries:   e.stats.DuplicateQueries,
		TotalVersions:      int64(versionCount),
		AvgVersionLen:      e.stats.AvgVersionLen,
	}
}

type VersioningStatsResponse struct {
	VersionsStored    int64 `json:"versions_stored"`
	VersionsCompared  int64 `json:"versions_compared"`
	DuplicateQueries  int64 `json:"duplicate_queries"`
	TotalVersions     int64 `json:"total_versions"`
	AvgVersionLen     int   `json:"avg_version_len"`
}