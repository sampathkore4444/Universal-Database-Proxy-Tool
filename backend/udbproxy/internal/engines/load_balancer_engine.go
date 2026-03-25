package engines

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/udbp/udbproxy/pkg/types"
)

// LoadBalancerEngine provides intelligent routing based on current DB load
type LoadBalancerEngine struct {
	BaseEngine
	config      *LoadBalanceConfig
	nodes       map[string]*LBNode
	strategy    string
	stats       *LoadBalanceStats
	mu          sync.RWMutex
}

type LoadBalanceConfig struct {
	Enabled           bool
	Strategy          string // round_robin, least_connections, weighted, least_latency
	HealthCheckMs     int
	MaxLatencyMs      int64
	WeightMap         map[string]int
}

type LBNode struct {
	Name          string
	Host          string
	Port          int
	Connections   int
	LatencyMs     int64
	Weight        int
	IsHealthy     bool
	LastQueryTime time.Time
}

type LoadBalanceStats struct {
	RequestsRouted   int64
	ActiveNodes      int64
	Failovers        int64
	AvgLatencyMs     float64
	mu               sync.RWMutex
}

// NewLoadBalancerEngine creates a new Load Balancer Engine
func NewLoadBalancerEngine(config *LoadBalanceConfig) *LoadBalancerEngine {
	if config == nil {
		config = &LoadBalanceConfig{
			Enabled:       false,
			Strategy:      "round_robin",
			HealthCheckMs: 5000,
			MaxLatencyMs:  5000,
		}
	}

	engine := &LoadBalancerEngine{
		BaseEngine: BaseEngine{name: "load_balancer"},
		config:     config,
		nodes:      make(map[string]*LBNode),
		strategy:   config.Strategy,
		stats:      &LoadBalanceStats{},
	}

	return engine
}

// AddNode adds a database node
func (e *LoadBalancerEngine) AddNode(node *LBNode) {
	e.mu.Lock()
	defer e.mu.Unlock()
	node.IsHealthy = true
	e.nodes[node.Name] = node
}

// Process determines target node for query
func (e *LoadBalancerEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	target := e.selectNode()
	
	if target == "" {
		return types.EngineResult{
			Continue: false,
			Error:    fmt.Errorf("no healthy nodes available"),
		}
	}

	// Update connection count
	e.mu.Lock()
	if node, ok := e.nodes[target]; ok {
		node.Connections++
		node.LastQueryTime = time.Now()
	}
	e.mu.Unlock()

	if qc.Metadata == nil {
		qc.Metadata = make(map[string]interface{})
	}
	qc.Metadata["load_balance_target"] = target
	qc.Metadata["load_balance_strategy"] = e.strategy

	e.stats.mu.Lock()
	e.stats.RequestsRouted++
	e.stats.mu.Unlock()

	return types.EngineResult{Continue: true}
}

// ProcessResponse updates node metrics
func (e *LoadBalancerEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if qc.Response != nil {
		target, _ := qc.Metadata["load_balance_target"].(string)
		
		e.mu.Lock()
		if node, ok := e.nodes[target]; ok {
			node.Connections--
			if qc.Response.Duration > 0 {
				node.LatencyMs = qc.Response.Duration.Milliseconds()
			}
		}
		e.mu.Unlock()

		if qc.Response.Duration > 0 {
			e.stats.mu.Lock()
			latency := float64(qc.Response.Duration.Milliseconds())
			count := e.stats.RequestsRouted
			e.stats.AvgLatencyMs = (e.stats.AvgLatencyMs*float64(count-1) + latency) / float64(count)
			e.stats.mu.Unlock()
		}
	}

	return types.EngineResult{Continue: true}
}

// selectNode selects best node based on strategy
func (e *LoadBalancerEngine) selectNode() string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var bestNode string
	var bestValue int64 = 0

	for name, node := range e.nodes {
		if !node.IsHealthy {
			continue
		}

		if node.LatencyMs > e.config.MaxLatencyMs {
			continue
		}

		var value int64
		switch e.strategy {
		case "round_robin":
			value = 0
		case "least_connections":
			value = int64(node.Connections)
		case "least_latency":
			value = node.LatencyMs
		case "weighted":
			value = int64(100 - node.Connections/node.Weight)
		default:
			value = 0
		}

		if bestNode == "" || value < bestValue {
			bestValue = value
			bestNode = name
		}
	}

	return bestNode
}

// GetLoadBalanceStats returns load balancer statistics
func (e *LoadBalancerEngine) GetLoadBalanceStats() LoadBalanceStatsResponse {
	e.stats.mu.RLock()
	defer e.stats.mu.RUnlock()

	e.mu.RLock()
	activeNodes := int64(len(e.nodes))
	e.mu.RUnlock()

	return LoadBalanceStatsResponse{
		RequestsRouted: e.stats.RequestsRouted,
		ActiveNodes:    activeNodes,
		Failovers:     e.stats.Failovers,
		AvgLatencyMs:  e.stats.AvgLatencyMs,
	}
}

type LoadBalanceStatsResponse struct {
	RequestsRouted int64   `json:"requests_routed"`
	ActiveNodes     int64   `json:"active_nodes"`
	Failovers       int64   `json:"failovers"`
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
}
