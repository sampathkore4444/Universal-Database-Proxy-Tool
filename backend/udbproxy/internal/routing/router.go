package routing

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/udbp/udbproxy/pkg/logger"
	"github.com/udbp/udbproxy/pkg/types"
)

type RoutingStrategy string

const (
	StrategyRoundRobin  RoutingStrategy = "round_robin"
	StrategyLeastConn   RoutingStrategy = "least_conn"
	StrategyReadReplica RoutingStrategy = "read_replica"
	StrategyLatency     RoutingStrategy = "latency"
	StrategyPriority    RoutingStrategy = "priority"
)

type DatabasePool struct {
	Name      string
	Database  *types.DatabaseConfig
	Weight    int
	Healthy   bool
	Latency   int64
	ConnCount int
	mu        sync.RWMutex
}

type Router struct {
	pools      map[string]*DatabasePool
	rules      []types.RoutingRule
	strategy   RoutingStrategy
	mu         sync.RWMutex
	rrCounters map[string]int
}

func NewRouter(rules []types.RoutingRule, strategy RoutingStrategy) *Router {
	return &Router{
		pools:      make(map[string]*DatabasePool),
		rules:      rules,
		strategy:   strategy,
		rrCounters: make(map[string]int),
	}
}

func (r *Router) RegisterDatabase(db *types.DatabaseConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.pools[db.Name] = &DatabasePool{
		Name:     db.Name,
		Database: db,
		Weight:   1,
		Healthy:  true,
	}
}

func (r *Router) UnregisterDatabase(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.pools, name)
}

func (r *Router) Route(ctx context.Context, qc *types.QueryContext) (*types.DatabaseConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	dbName := r.findMatchingDB(qc)
	if dbName == "" {
		return nil, fmt.Errorf("no matching database found for query")
	}

	pool := r.pools[dbName]
	if pool == nil {
		return nil, fmt.Errorf("database pool not found: %s", dbName)
	}

	if !pool.Healthy {
		fallback := r.findFallback(dbName)
		if fallback != nil {
			logger.Warn("Using fallback database")
			return fallback.Database, nil
		}
		return nil, fmt.Errorf("database unhealthy: %s", dbName)
	}

	selected := r.selectFromPool(pool, qc)
	return selected, nil
}

func (r *Router) findMatchingDB(qc *types.QueryContext) string {
	sortedRules := make([]types.RoutingRule, len(r.rules))
	copy(sortedRules, r.rules)
	sort.Slice(sortedRules, func(i, j int) bool {
		return sortedRules[i].Priority > sortedRules[j].Priority
	})

	for _, rule := range sortedRules {
		if r.matchRule(rule, qc) {
			return rule.Database
		}
	}

	return ""
}

func (r *Router) matchRule(rule types.RoutingRule, qc *types.QueryContext) bool {
	if rule.MatchPattern == "" {
		return true
	}

	pattern := "(?i)" + rule.MatchPattern
	matched, err := regexp.MatchString(pattern, qc.RawQuery)
	return err == nil && matched
}

func (r *Router) selectFromPool(pool *DatabasePool, qc *types.QueryContext) *types.DatabaseConfig {
	switch r.strategy {
	case StrategyRoundRobin:
		return r.roundRobinSelect(pool)
	case StrategyLeastConn:
		return r.leastConnSelect(pool)
	case StrategyReadReplica:
		return r.readReplicaSelect(pool, qc)
	case StrategyLatency:
		return r.latencySelect(pool)
	default:
		return pool.Database
	}
}

func (r *Router) roundRobinSelect(pool *DatabasePool) *types.DatabaseConfig {
	r.rrCounters[pool.Name]++
	count := r.rrCounters[pool.Name]
	if count%pool.Weight == 0 {
		return pool.Database
	}
	return pool.Database
}

func (r *Router) leastConnSelect(pool *DatabasePool) *types.DatabaseConfig {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	return pool.Database
}

func (r *Router) readReplicaSelect(pool *DatabasePool, qc *types.QueryContext) *types.DatabaseConfig {
	if qc.Operation == types.OpSelect && pool.Database.IsReadReplica {
		return pool.Database
	}
	return pool.Database
}

func (r *Router) latencySelect(pool *DatabasePool) *types.DatabaseConfig {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	return pool.Database
}

func (r *Router) findFallback(exclude string) *DatabasePool {
	for _, pool := range r.pools {
		if pool.Name != exclude && pool.Healthy {
			return pool
		}
	}
	return nil
}

func (r *Router) SetDatabaseHealth(name string, healthy bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if pool, ok := r.pools[name]; ok {
		pool.mu.Lock()
		pool.Healthy = healthy
		pool.mu.Unlock()
	}
}

func (r *Router) SetDatabaseLatency(name string, latency int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if pool, ok := r.pools[name]; ok {
		pool.mu.Lock()
		pool.Latency = latency
		pool.mu.Unlock()
	}
}

func (r *Router) GetDatabaseStats(name string) (int, int64, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if pool, ok := r.pools[name]; ok {
		pool.mu.RLock()
		defer pool.mu.RUnlock()
		return pool.ConnCount, pool.Latency, pool.Healthy
	}
	return 0, 0, false
}

func (r *Router) AddRule(rule types.RoutingRule) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rules = append(r.rules, rule)
}

func (r *Router) RemoveRule(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var newRules []types.RoutingRule
	for _, r := range r.rules {
		if r.Name != name {
			newRules = append(newRules, r)
		}
	}
	r.rules = newRules
}

func (r *Router) SetStrategy(strategy RoutingStrategy) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.strategy = strategy
}

func (r *Router) GetPools() map[string]*DatabasePool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]*DatabasePool)
	for k, v := range r.pools {
		v.mu.RLock()
		result[k] = &DatabasePool{
			Name:      v.Name,
			Database:  v.Database,
			Weight:    v.Weight,
			Healthy:   v.Healthy,
			Latency:   v.Latency,
			ConnCount: v.ConnCount,
		}
		v.mu.RUnlock()
	}
	return result
}

type CircuitBreaker struct {
	failures    int
	threshold   int
	timeout     int64
	lastFailure int64
	state       CircuitState
	mu          sync.Mutex
}

type CircuitState int

const (
	StateClosed CircuitState = iota
	StateOpen
	StateHalfOpen
)

func NewCircuitBreaker(threshold int, timeoutSeconds int64) *CircuitBreaker {
	return &CircuitBreaker{
		threshold: threshold,
		timeout:   timeoutSeconds,
		state:     StateClosed,
	}
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0
	cb.state = StateClosed
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailure = 0

	if cb.failures >= cb.threshold {
		cb.state = StateOpen
	}
}

func (cb *CircuitBreaker) AllowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		if cb.lastFailure > 0 {
			return true
		}
		return false
	case StateHalfOpen:
		return true
	default:
		return false
	}
}

func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

func ParseOperation(query string) types.QueryOperation {
	query = strings.TrimSpace(strings.ToUpper(query))

	if strings.HasPrefix(query, "SELECT") {
		return types.OpSelect
	}
	if strings.HasPrefix(query, "INSERT") {
		return types.OpInsert
	}
	if strings.HasPrefix(query, "UPDATE") {
		return types.OpUpdate
	}
	if strings.HasPrefix(query, "DELETE") {
		return types.OpDelete
	}
	return types.OpOther
}
