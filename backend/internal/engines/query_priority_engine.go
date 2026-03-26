package engines

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/udbp/udbproxy/pkg/types"
)

type QueryPriorityEngine struct {
	BaseEngine
	config          *PriorityConfig
	queues          map[PriorityLevel]*PriorityQueue
	defaultPriority PriorityLevel
	mu              sync.RWMutex
}

type PriorityConfig struct {
	Enabled           bool
	DefaultPriority   PriorityLevel
	MaxQueueSize      int
	StarvationTimeout time.Duration
}

type PriorityLevel int

const (
	PriorityLow PriorityLevel = iota
	PriorityNormal
	PriorityHigh
	PriorityCritical
)

type PriorityQueue struct {
	level PriorityLevel
	items []*types.QueryContext
	mu    sync.Mutex
}

func NewQueryPriorityEngine(config *PriorityConfig) *QueryPriorityEngine {
	if config == nil {
		config = &PriorityConfig{
			Enabled:           true,
			DefaultPriority:   PriorityNormal,
			MaxQueueSize:      10000,
			StarvationTimeout: 30 * time.Second,
		}
	}

	engine := &QueryPriorityEngine{
		BaseEngine:      BaseEngine{name: "query_priority"},
		config:          config,
		queues:          make(map[PriorityLevel]*PriorityQueue),
		defaultPriority: config.DefaultPriority,
	}

	engine.queues[PriorityCritical] = &PriorityQueue{level: PriorityCritical}
	engine.queues[PriorityHigh] = &PriorityQueue{level: PriorityHigh}
	engine.queues[PriorityNormal] = &PriorityQueue{level: PriorityNormal}
	engine.queues[PriorityLow] = &PriorityQueue{level: PriorityLow}

	return engine
}

func (e *QueryPriorityEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	priority := e.determinePriority(qc)

	qc.Metadata["query_priority"] = priority

	return types.EngineResult{Continue: true}
}

func (e *QueryPriorityEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	return types.EngineResult{Continue: true}
}

func (e *QueryPriorityEngine) determinePriority(qc *types.QueryContext) PriorityLevel {
	if priority, ok := qc.Metadata["requested_priority"].(PriorityLevel); ok {
		return priority
	}

	switch qc.Operation {
	case types.OpSelect:
		return PriorityNormal
	case types.OpInsert:
		return PriorityHigh
	case types.OpUpdate:
		return PriorityHigh
	case types.OpDelete:
		return PriorityCritical
	default:
		return e.defaultPriority
	}
}

func (e *QueryPriorityEngine) Enqueue(qc *types.QueryContext) error {
	priority := e.determinePriority(qc)

	queue, ok := e.queues[priority]
	if !ok {
		return fmt.Errorf("invalid priority level: %d", priority)
	}

	queue.mu.Lock()
	defer queue.mu.Unlock()

	if len(queue.items) >= e.config.MaxQueueSize {
		return fmt.Errorf("queue is full")
	}

	queue.items = append(queue.items, qc)
	return nil
}

func (e *QueryPriorityEngine) Dequeue() *types.QueryContext {
	levels := []PriorityLevel{PriorityCritical, PriorityHigh, PriorityNormal, PriorityLow}

	for _, level := range levels {
		queue, ok := e.queues[level]
		if !ok {
			continue
		}

		queue.mu.Lock()
		if len(queue.items) > 0 {
			item := queue.items[0]
			queue.items = queue.items[1:]
			queue.mu.Unlock()
			return item
		}
		queue.mu.Unlock()
	}

	return nil
}

func (e *QueryPriorityEngine) GetQueueSize(priority PriorityLevel) int {
	queue, ok := e.queues[priority]
	if !ok {
		return 0
	}

	queue.mu.Lock()
	defer queue.mu.Unlock()
	return len(queue.items)
}

func (e *QueryPriorityEngine) GetAllQueueSizes() map[PriorityLevel]int {
	result := make(map[PriorityLevel]int)

	for level, queue := range e.queues {
		queue.mu.Lock()
		result[level] = len(queue.items)
		queue.mu.Unlock()
	}

	return result
}

func (e *QueryPriorityEngine) SetPriority(queryID string, priority PriorityLevel) error {
	for _, queue := range e.queues {
		queue.mu.Lock()
		for i, qc := range queue.items {
			if qc.ID == queryID {
				queue.items = append(queue.items[:i], queue.items[i+1:]...)
				queue.mu.Unlock()

				targetQueue, ok := e.queues[priority]
				if !ok {
					return fmt.Errorf("invalid priority level")
				}

				targetQueue.mu.Lock()
				targetQueue.items = append(targetQueue.items, qc)
				targetQueue.mu.Unlock()

				return nil
			}
		}
		queue.mu.Unlock()
	}

	return fmt.Errorf("query not found: %s", queryID)
}

func (e *QueryPriorityEngine) ClearQueues() {
	for _, queue := range e.queues {
		queue.mu.Lock()
		queue.items = nil
		queue.mu.Unlock()
	}
}
