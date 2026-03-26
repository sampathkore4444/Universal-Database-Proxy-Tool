package engines

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/udbp/udbproxy/pkg/types"
)

type PoolRebalancerEngine struct {
	BaseEngine
	config        *RebalancerConfig
	pools         map[string]*PoolInfo
	mu            sync.RWMutex
	lastRebalance time.Time
}

type RebalancerConfig struct {
	Enabled            bool
	Interval           time.Duration
	MinConnections     int
	MaxConnections     int
	TargetUtilization  float64
	AutoScale          bool
	ScaleUpThreshold   float64
	ScaleDownThreshold float64
}

type PoolInfo struct {
	Name          string
	MinConns      int
	MaxConns      int
	ActiveConns   int
	IdleConns     int
	WaitCount     int64
	AvgWaitTime   time.Duration
	LastRebalance time.Time
	Database      string
	mu            sync.RWMutex
}

func NewPoolRebalancerEngine(config *RebalancerConfig) *PoolRebalancerEngine {
	if config == nil {
		config = &RebalancerConfig{
			Enabled:            true,
			Interval:           30 * time.Second,
			MinConnections:     5,
			MaxConnections:     100,
			TargetUtilization:  0.7,
			AutoScale:          true,
			ScaleUpThreshold:   0.8,
			ScaleDownThreshold: 0.3,
		}
	}

	engine := &PoolRebalancerEngine{
		BaseEngine: BaseEngine{name: "pool_rebalancer"},
		config:     config,
		pools:      make(map[string]*PoolInfo),
	}

	if config.Enabled {
		go engine.runRebalancer()
	}

	return engine
}

func (e *PoolRebalancerEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	return types.EngineResult{Continue: true}
}

func (e *PoolRebalancerEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	poolName, ok := qc.Metadata["pool_name"].(string)
	if !ok {
		return types.EngineResult{Continue: true}
	}

	e.mu.RLock()
	pool, exists := e.pools[poolName]
	e.mu.RUnlock()

	if !exists {
		return types.EngineResult{Continue: true}
	}

	pool.mu.Lock()
	pool.ActiveConns--
	pool.IdleConns++
	pool.mu.Unlock()

	return types.EngineResult{Continue: true}
}

func (e *PoolRebalancerEngine) runRebalancer() {
	ticker := time.NewTicker(e.config.Interval)
	defer ticker.Stop()

	for range ticker.C {
		e.rebalance()
	}
}

func (e *PoolRebalancerEngine) rebalance() {
	if !e.config.AutoScale {
		return
	}

	e.mu.RLock()
	pools := make([]*PoolInfo, 0, len(e.pools))
	for _, p := range e.pools {
		pools = append(pools, p)
	}
	e.mu.RUnlock()

	for _, pool := range pools {
		e.analyzeAndAdjust(pool)
	}

	e.lastRebalance = time.Now()
}

func (e *PoolRebalancerEngine) analyzeAndAdjust(pool *PoolInfo) {
	pool.mu.RLock()
	utilization := float64(pool.ActiveConns) / float64(pool.MaxConns)
	pool.mu.RUnlock()

	if utilization > e.config.ScaleUpThreshold && pool.MaxConns < e.config.MaxConnections {
		newMax := pool.MaxConns + 10
		if newMax > e.config.MaxConnections {
			newMax = e.config.MaxConnections
		}
		pool.mu.Lock()
		pool.MaxConns = newMax
		pool.LastRebalance = time.Now()
		pool.mu.Unlock()
	}

	if utilization < e.config.ScaleDownThreshold && pool.MinConns > e.config.MinConnections {
		newMin := pool.MinConns - 2
		if newMin < e.config.MinConnections {
			newMin = e.config.MinConnections
		}
		pool.mu.Lock()
		pool.MinConns = newMin
		pool.LastRebalance = time.Now()
		pool.mu.Unlock()
	}
}

func (e *PoolRebalancerEngine) RegisterPool(name string, minConns, maxConns int, database string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.pools[name] = &PoolInfo{
		Name:          name,
		MinConns:      minConns,
		MaxConns:      maxConns,
		ActiveConns:   0,
		IdleConns:     minConns,
		Database:      database,
		LastRebalance: time.Now(),
	}
}

func (e *PoolRebalancerEngine) UnregisterPool(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.pools, name)
}

func (e *PoolRebalancerEngine) RecordConnectionAcquired(poolName string) error {
	e.mu.RLock()
	pool, ok := e.pools[poolName]
	e.mu.RUnlock()

	if !ok {
		return fmt.Errorf("pool not found: %s", poolName)
	}

	pool.mu.Lock()
	pool.ActiveConns++
	pool.IdleConns--
	pool.mu.Unlock()

	return nil
}

func (e *PoolRebalancerEngine) RecordConnectionReleased(poolName string) error {
	e.mu.RLock()
	pool, ok := e.pools[poolName]
	e.mu.RUnlock()

	if !ok {
		return fmt.Errorf("pool not found: %s", poolName)
	}

	pool.mu.Lock()
	pool.ActiveConns--
	pool.IdleConns++
	pool.mu.Unlock()

	return nil
}

func (e *PoolRebalancerEngine) RecordWaitTime(poolName string, waitTime time.Duration) error {
	e.mu.RLock()
	pool, ok := e.pools[poolName]
	e.mu.RUnlock()

	if !ok {
		return fmt.Errorf("pool not found: %s", poolName)
	}

	pool.mu.Lock()
	pool.WaitCount++
	if pool.AvgWaitTime == 0 {
		pool.AvgWaitTime = waitTime
	} else {
		pool.AvgWaitTime = (pool.AvgWaitTime + waitTime) / 2
	}
	pool.mu.Unlock()

	return nil
}

func (e *PoolRebalancerEngine) GetPoolStats(poolName string) (*PoolInfo, error) {
	e.mu.RLock()
	pool, ok := e.pools[poolName]
	e.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("pool not found: %s", poolName)
	}

	pool.mu.RLock()
	defer pool.mu.RUnlock()

	return &PoolInfo{
		Name:          pool.Name,
		MinConns:      pool.MinConns,
		MaxConns:      pool.MaxConns,
		ActiveConns:   pool.ActiveConns,
		IdleConns:     pool.IdleConns,
		WaitCount:     pool.WaitCount,
		AvgWaitTime:   pool.AvgWaitTime,
		LastRebalance: pool.LastRebalance,
	}, nil
}

func (e *PoolRebalancerEngine) GetAllPoolsStats() map[string]*PoolInfo {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make(map[string]*PoolInfo)
	for name, pool := range e.pools {
		pool.mu.RLock()
		result[name] = &PoolInfo{
			Name:          pool.Name,
			MinConns:      pool.MinConns,
			MaxConns:      pool.MaxConns,
			ActiveConns:   pool.ActiveConns,
			IdleConns:     pool.IdleConns,
			WaitCount:     pool.WaitCount,
			AvgWaitTime:   pool.AvgWaitTime,
			LastRebalance: pool.LastRebalance,
		}
		pool.mu.RUnlock()
	}
	return result
}

func (e *PoolRebalancerEngine) GetLastRebalanceTime() time.Time {
	return e.lastRebalance
}

func (e *PoolRebalancerEngine) ForceRebalance() {
	e.rebalance()
}

func (e *PoolRebalancerEngine) SetConfig(config *RebalancerConfig) {
	e.config = config
}

func (e *PoolRebalancerEngine) GetConfig() *RebalancerConfig {
	return e.config
}
