package engines

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/udbp/udbproxy/pkg/types"
)

type QueryTimeoutEngine struct {
	BaseEngine
	config        *TimeoutConfig
	activeQueries map[string]*QueryContext
	mu            sync.RWMutex
}

type TimeoutConfig struct {
	Enabled            bool
	DefaultTimeout     time.Duration
	PerQueryTimeout    map[string]time.Duration
	KillQueryOnTimeout bool
	SlowQueryThreshold time.Duration
}

func NewQueryTimeoutEngine(config *TimeoutConfig) *QueryTimeoutEngine {
	if config == nil {
		config = &TimeoutConfig{
			Enabled:            true,
			DefaultTimeout:     30 * time.Second,
			SlowQueryThreshold: 5 * time.Second,
		}
	}

	return &QueryTimeoutEngine{
		BaseEngine:    BaseEngine{name: "query_timeout"},
		config:        config,
		activeQueries: make(map[string]*QueryContext),
	}
}

func (e *QueryTimeoutEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	timeout := e.getTimeout(qc)

	qcWithTimeout, cancel := context.WithTimeout(ctx, timeout)
	qc.Metadata["timeout_context"] = qcWithTimeout
	qc.Metadata["timeout_cancel"] = cancel

	e.mu.Lock()
	e.activeQueries[qc.ID] = qc
	e.mu.Unlock()

	go e.monitorQuery(qc, timeout)

	return types.EngineResult{Continue: true}
}

func (e *QueryTimeoutEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	e.mu.Lock()
	delete(e.activeQueries, qc.ID)
	e.mu.Unlock()

	if cancel, ok := qc.Metadata["timeout_cancel"].(context.CancelFunc); ok {
		cancel()
	}

	return types.EngineResult{Continue: true}
}

func (e *QueryTimeoutEngine) getTimeout(qc *types.QueryContext) time.Duration {
	if perQuery, ok := e.config.PerQueryTimeout[string(qc.Operation)]; ok {
		return perQuery
	}

	switch qc.Operation {
	case types.OpSelect:
		return e.config.DefaultTimeout * 2
	case types.OpInsert, types.OpUpdate:
		return e.config.DefaultTimeout
	case types.OpDelete:
		return e.config.DefaultTimeout
	default:
		return e.config.DefaultTimeout
	}
}

func (e *QueryTimeoutEngine) monitorQuery(qc *types.QueryContext, timeout time.Duration) {
	select {
	case <-time.After(timeout):
		e.handleTimeout(qc)
	case <-time.After(e.config.SlowQueryThreshold):
		e.handleSlowQuery(qc)
	}
}

func (e *QueryTimeoutEngine) handleTimeout(qc *types.QueryContext) {
	e.mu.Lock()
	_, exists := e.activeQueries[qc.ID]
	e.mu.Unlock()

	if exists && e.config.KillQueryOnTimeout {
		qc.Metadata["timeout_killed"] = true
	}
}

func (e *QueryTimeoutEngine) handleSlowQuery(qc *types.QueryContext) {
	qc.Metadata["slow_query_detected"] = true
}

func (e *QueryTimeoutEngine) GetActiveQueries() []*QueryContext {
	e.mu.RLock()
	defer e.mu.RUnlock()

	queries := make([]*QueryContext, 0, len(e.activeQueries))
	for _, q := range e.activeQueries {
		queries = append(queries, q)
	}
	return queries
}

func (e *QueryTimeoutEngine) CancelQuery(queryID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if qc, ok := e.activeQueries[queryID]; ok {
		if cancel, ok := qc.Metadata["timeout_cancel"].(context.CancelFunc); ok {
			cancel()
			delete(e.activeQueries, queryID)
			return nil
		}
	}
	return fmt.Errorf("query not found or already completed")
}

func (e *QueryTimeoutEngine) SetConfig(config *TimeoutConfig) {
	e.config = config
}

func (e *QueryTimeoutEngine) GetConfig() *TimeoutConfig {
	return e.config
}
