package engines

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/udbp/udbproxy/pkg/types"
)

// GraphEngine handles optimized traversal queries for relationship data
type GraphEngine struct {
	BaseEngine
	config          *GraphConfig
	nodeCache       map[string]*GraphNode
	edgeCache       map[string]*GraphEdge
	traversalCache  map[string]*TraversalResult
	stats           *GraphStats
	mu              sync.RWMutex
}

type GraphConfig struct {
	Enabled            bool
	MaxTraversalDepth  int
	EnableShortestPath bool
	EnableCycles       bool
	CacheEnabled       bool
}

type GraphNode struct {
	ID        string
	Type      string
	Properties map[string]interface{}
}

type GraphEdge struct {
	From      string
	To        string
	Type      string
	Weight    float64
	Direction string
}

type TraversalResult struct {
	QueryID    string
	Path       []string
	TotalCost  float64
	NodesVisited int
	Duration   time.Duration
}

type GraphStats struct {
	TraversalQueries   int64
	OptimizedQueries   int64
	CacheHits         int64
	CacheMisses       int64
	AvgTraversalMs    float64
	mu                sync.RWMutex
}

// NewGraphEngine creates a new Graph Engine
func NewGraphEngine(config *GraphConfig) *GraphEngine {
	if config == nil {
		config = &GraphConfig{
			Enabled:            true,
			MaxTraversalDepth: 10,
			EnableShortestPath: true,
			EnableCycles:       false,
			CacheEnabled:       true,
		}
	}

	engine := &GraphEngine{
		BaseEngine:     BaseEngine{name: "graph"},
		config:         config,
		nodeCache:      make(map[string]*GraphNode),
		edgeCache:      make(map[string]*GraphEdge),
		traversalCache: make(map[string]*TraversalResult),
		stats:          &GraphStats{},
	}

	return engine
}

// Process handles graph query optimization
func (e *GraphEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	query := strings.TrimSpace(qc.RawQuery)
	if query == "" {
		return types.EngineResult{Continue: true}
	}

	upperQuery := strings.ToUpper(query)

	// Detect graph traversal patterns
	if !e.isGraphQuery(upperQuery) {
		return types.EngineResult{Continue: true}
	}

	e.stats.mu.Lock()
	e.stats.TraversalQueries++
	e.stats.mu.Unlock()

	// Check cache
	if e.config.CacheEnabled {
		cacheKey := e.hashQuery(query)
		if result, ok := e.traversalCache[cacheKey]; ok {
			e.stats.mu.Lock()
			e.stats.CacheHits++
			e.stats.mu.Unlock()

			if qc.Metadata == nil {
				qc.Metadata = make(map[string]interface{})
			}
			qc.Metadata["graph_cached"] = true
			qc.Metadata["cached_result"] = result
		}
	}

	// Optimize graph query
	optimized := e.optimizeGraphQuery(query)
	if optimized != query {
		qc.RawQuery = optimized
		
		e.stats.mu.Lock()
		e.stats.OptimizedQueries++
		e.stats.mu.Unlock()

		if qc.Metadata == nil {
			qc.Metadata = make(map[string]interface{})
		}
		qc.Metadata["graph_optimized"] = true
	}

	return types.EngineResult{Continue: true}
}

// ProcessResponse handles graph response
func (e *GraphEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if qc.Response != nil && qc.Response.Duration > 0 && e.config.CacheEnabled {
		// Cache result
		cacheKey := e.hashQuery(qc.RawQuery)
		
		result := &TraversalResult{
			QueryID:     qc.ID,
			Path:        []string{},
			TotalCost:   0,
			NodesVisited: int(qc.Response.RowsReturned),
			Duration:    qc.Response.Duration,
		}

		e.mu.Lock()
		e.traversalCache[cacheKey] = result
		e.mu.Unlock()
	}

	return types.EngineResult{Continue: true}
}

// isGraphQuery detects graph-related queries
func (e *GraphEngine) isGraphQuery(query string) bool {
	patterns := []string{
		"RECURSIVE",
		"CONNECT BY",
		"WITH RECURSIVE",
		"MATCH",
		"PATH",
		"SHORTEST",
		"TRAVERSAL",
		"FRIEND OF",
		"FOLLOWS",
		"RELATED TO",
	}

	for _, pattern := range patterns {
		if strings.Contains(query, pattern) {
			return true
		}
	}

	return false
}

// optimizeGraphQuery optimizes graph queries
func (e *GraphEngine) optimizeGraphQuery(query string) string {
	upper := strings.ToUpper(query)

	// Add depth limit if not present
	if strings.Contains(upper, "RECURSIVE") || strings.Contains(upper, "CONNECT BY") {
		re := regexp.MustCompile(`(?i)CONNECT\s+BY`)
		if !re.MatchString(query) || !strings.Contains(upper, "LIMIT") {
			// Add LIMIT for safety
			return query + " LIMIT 1000"
		}
	}

	return query
}

// hashQuery creates a hash for caching
func (e *GraphEngine) hashQuery(query string) string {
	hash := 0
	for _, c := range query {
		hash = hash*31 + int(c)
	}
	return string(rune(hash))
}

// AddNode adds a graph node
func (e *GraphEngine) AddNode(node *GraphNode) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.nodeCache[node.ID] = node
}

// AddEdge adds a graph edge
func (e *GraphEngine) AddEdge(edge *GraphEdge) {
	e.mu.Lock()
	defer e.mu.Unlock()
	key := fmt.Sprintf("%s->%s", edge.From, edge.To)
	e.edgeCache[key] = edge
}

// GetGraphStats returns graph statistics
func (e *GraphEngine) GetGraphStats() GraphStatsResponse {
	e.stats.mu.RLock()
	defer e.stats.mu.RUnlock()

	return GraphStatsResponse{
		TraversalQueries:   e.stats.TraversalQueries,
		OptimizedQueries:   e.stats.OptimizedQueries,
		CacheHits:          e.stats.CacheHits,
		CacheMisses:        e.stats.CacheMisses,
		AvgTraversalMs:     e.stats.AvgTraversalMs,
	}
}

type GraphStatsResponse struct {
	TraversalQueries int64   `json:"traversal_queries"`
	OptimizedQueries int64   `json:"optimized_queries"`
	CacheHits        int64   `json:"cache_hits"`
	CacheMisses      int64   `json:"cache_misses"`
	AvgTraversalMs   float64 `json:"avg_traversal_ms"`
}