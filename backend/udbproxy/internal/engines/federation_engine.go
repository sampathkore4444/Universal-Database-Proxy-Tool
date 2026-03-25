package engines

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/udbp/udbproxy/pkg/types"
)

// FederationEngine handles cross-database queries and sharding-aware routing
type FederationEngine struct {
	BaseEngine
	config           *FederationConfig
	shardMap         map[string]*Shard
	shardingStrategy ShardingStrategy
	stats            *FederationStats
	mu               sync.RWMutex
}

type FederationConfig struct {
	Enabled              bool
	ShardKeyPatterns     []string
	DefaultShard         string
	CrossShardQueries    bool // Allow queries across shards
	AggregationEnabled   bool
	MaxShardsPerQuery    int
}

type Shard struct {
	ID       string
	Name     string
	Database string
	Host     string
	Port     int
	Tables   []string
	ShardKey string
	Status   string
	Weight   int // For load balancing
}

type ShardingStrategy interface {
	GetShard(shardKey string) (*Shard, error)
	GetAllShards() []*Shard
}

type HashShardingStrategy struct {
	shards []*Shard
	vnodes int
}

type RangeShardingStrategy struct {
	shards []*Shard
	ranges []string
}

type FederationStats struct {
	CrossShardQueries     int64
	SingleShardQueries   int64
	AggregatedQueries     int64
	RoutingErrors        int64
	AvgLatencyMs         float64
	mu                   sync.RWMutex
}

// NewFederationEngine creates a new Federation Engine
func NewFederationEngine(config *FederationConfig) *FederationEngine {
	if config == nil {
		config = &FederationConfig{
			Enabled:           true,
			CrossShardQueries: true,
			AggregationEnabled: true,
			MaxShardsPerQuery: 10,
		}
	}

	engine := &FederationEngine{
		BaseEngine: BaseEngine{name: "federation"},
		config:     config,
		shardMap:   make(map[string]*Shard),
		stats:      &FederationStats{},
	}

	return engine
}

// AddShard adds a shard to the federation
func (e *FederationEngine) AddShard(shard *Shard) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.shardMap[shard.ID] = shard
}

// Process handles federation routing
func (e *FederationEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	query := strings.TrimSpace(qc.RawQuery)
	if query == "" {
		return types.EngineResult{Continue: true}
	}

	upperQuery := strings.ToUpper(query)
	if !strings.HasPrefix(upperQuery, "SELECT") && !strings.HasPrefix(upperQuery, "WITH") {
		return types.EngineResult{Continue: true}
	}

	// Detect sharding key from query
	shardKey := e.extractShardKey(query)
	
	// Determine target shards
	targetShards := e.determineTargetShard(shardKey, query)
	
	if len(targetShards) == 0 {
		e.stats.mu.Lock()
		e.stats.RoutingErrors++
		e.stats.mu.Unlock()
		return types.EngineResult{
			Continue: false,
			Error:    fmt.Errorf("no shards available for query"),
		}
	}

	// Update query context with federation metadata
	if qc.Metadata == nil {
		qc.Metadata = make(map[string]interface{})
	}
	
	shardIDs := make([]string, len(targetShards))
	for i, shard := range targetShards {
		shardIDs[i] = shard.ID
	}
	
	qc.Metadata["federation_enabled"] = true
	qc.Metadata["target_shards"] = shardIDs
	qc.Metadata["cross_shard"] = len(targetShards) > 1
	qc.Metadata["shard_key"] = shardKey

	e.stats.mu.Lock()
	if len(targetShards) > 1 {
		e.stats.CrossShardQueries++
	} else {
		e.stats.SingleShardQueries++
	}
	e.stats.mu.Unlock()

	return types.EngineResult{Continue: true}
}

// ProcessResponse processes federation response
func (e *FederationEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if qc.Response != nil && qc.Response.Duration > 0 {
		e.stats.mu.Lock()
		latency := float64(qc.Response.Duration.Milliseconds())
		e.stats.AvgLatencyMs = (e.stats.AvgLatencyMs*float64(e.stats.SingleShardQueries+e.stats.CrossShardQueries-1) + latency) / float64(e.stats.SingleShardQueries+e.stats.CrossShardQueries)
		e.stats.mu.Unlock()
	}
	return types.EngineResult{Continue: true}
}

// extractShardKey extracts sharding key from query
func (e *FederationEngine) extractShardKey(query string) string {
	for _, pattern := range e.config.ShardKeyPatterns {
		re := regexp.MustCompile(fmt.Sprintf(`(?i)%s\s*=`, pattern))
		if re.MatchString(query) {
			return pattern
		}
		
		// Check for WHERE clause with shard key
		re = regexp.MustCompile(fmt.Sprintf(`(?i)WHERE\s+%s\s*=\s*['"]?([^'\s"]+)['"]?`, pattern))
		matches := re.FindStringSubmatch(query)
		if len(matches) > 1 {
			return matches[1]
		}
	}
	return ""
}

// determineTargetShards determines which shards to query
func (e *FederationEngine) determineTargetShard(shardKey string, query string) []*Shard {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// If no shard key provided, query all shards (if enabled)
	if shardKey == "" {
		if e.config.CrossShardQueries {
			shards := make([]*Shard, 0, len(e.shardMap))
			for _, shard := range e.shardMap {
				shards = append(shards, shard)
			}
			return shards
		}
		// Use default shard
		if defaultShard, ok := e.shardMap[e.config.DefaultShard]; ok {
			return []*Shard{defaultShard}
		}
		return nil
	}

	// Hash-based sharding
	shardIndex := hashString(shardKey) % uint32(len(e.shardMap))
	
	i := 0
	for _, shard := range e.shardMap {
		if int(shardIndex) == i {
			return []*Shard{shard}
		}
		i++
	}
	
	// Fallback to first shard
	for _, shard := range e.shardMap {
		return []*Shard{shard}
	}
	
	return nil
}

// Simple hash function for sharding
func hashString(s string) uint32 {
	var hash uint32
	for _, c := range s {
		hash = hash*31 + uint32(c)
	}
	return hash
}

// GetFederationStats returns federation statistics
func (e *FederationEngine) GetFederationStats() FederationStatsResponse {
	e.stats.mu.RLock()
	defer e.stats.mu.RUnlock()

	return FederationStatsResponse{
		CrossShardQueries:  e.stats.CrossShardQueries,
		SingleShardQueries: e.stats.SingleShardQueries,
		AggregatedQueries:  e.stats.AggregatedQueries,
		RoutingErrors:     e.stats.RoutingErrors,
		AvgLatencyMs:      e.stats.AvgLatencyMs,
	}
}

// GetShards returns all configured shards
func (e *FederationEngine) GetShards() []Shard {
	e.mu.RLock()
	defer e.mu.RUnlock()

	shards := make([]Shard, 0, len(e.shardMap))
	for _, shard := range e.shardMap {
		shards = append(shards, *shard)
	}
	return shards
}

type FederationStatsResponse struct {
	CrossShardQueries  int64   `json:"cross_shard_queries"`
	SingleShardQueries int64   `json:"single_shard_queries"`
	AggregatedQueries  int64   `json:"aggregated_queries"`
	RoutingErrors     int64   `json:"routing_errors"`
	AvgLatencyMs      float64 `json:"avg_latency_ms"`
}