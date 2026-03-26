package engines

import (
	"context"
	"fmt"
	"hash/fnv"
	"regexp"
	"sync"

	"github.com/udbp/udbproxy/pkg/types"
)

type ShardingEngine struct {
	BaseEngine
	config     *ShardingConfig
	shards     map[int]*Shard
	shardRules []ShardRule
	mu         sync.RWMutex
}

type ShardingConfig struct {
	Enabled           bool
	DefaultShardCount int
	ShardKeyPattern   string
	VirtualShards     int
}

type Shard struct {
	ID       int
	Name     string
	Host     string
	Port     int
	Database string
	Weight   int
	Healthy  bool
	mu       sync.RWMutex
}

type ShardRule struct {
	Name         string
	TablePattern string
	ShardKey     string
	ShardType    ShardType
	RangeStart   int
	RangeEnd     int
}

type ShardType string

const (
	ShardTypeRange      ShardType = "range"
	ShardTypeHash       ShardType = "hash"
	ShardTypeList       ShardType = "list"
	ShardTypeRoundRobin ShardType = "round_robin"
)

func NewShardingEngine(config *ShardingConfig) *ShardingEngine {
	if config == nil {
		config = &ShardingConfig{
			Enabled:           true,
			DefaultShardCount: 4,
			VirtualShards:     256,
		}
	}

	return &ShardingEngine{
		BaseEngine: BaseEngine{name: "sharding"},
		config:     config,
		shards:     make(map[int]*Shard),
	}
}

func (e *ShardingEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	shardID, err := e.determineShard(qc)
	if err != nil {
		return types.EngineResult{
			Continue: false,
			Error:    err,
		}
	}

	qc.Metadata["shard_id"] = shardID
	qc.Database = fmt.Sprintf("%s_shard%d", qc.Database, shardID)

	return types.EngineResult{Continue: true}
}

func (e *ShardingEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	return types.EngineResult{Continue: true}
}

func (e *ShardingEngine) determineShard(qc *types.QueryContext) (int, error) {
	shardKey := e.extractShardKey(qc)
	if shardKey == "" {
		return 0, nil
	}

	rule := e.findMatchingRule(qc.Table)
	if rule == nil {
		return e.hashShard(shardKey), nil
	}

	switch rule.ShardType {
	case ShardTypeRange:
		return e.rangeShard(shardKey, rule)
	case ShardTypeHash:
		return e.hashShard(shardKey), nil
	case ShardTypeList:
		return e.listShard(shardKey, rule)
	default:
		return e.hashShard(shardKey), nil
	}
}

func (e *ShardingEngine) extractShardKey(qc *types.QueryContext) string {
	if key, ok := qc.Metadata["shard_key"].(string); ok {
		return key
	}

	re := regexp.MustCompile(`(?i)WHERE\s+(\w+)\s*=`)
	matches := re.FindStringSubmatch(qc.RawQuery)
	if len(matches) > 1 {
		return matches[1]
	}

	return ""
}

func (e *ShardingEngine) findMatchingRule(table string) *ShardRule {
	if table == "" {
		return nil
	}

	for _, rule := range e.shardRules {
		matched, _ := regexp.MatchString(rule.TablePattern, table)
		if matched {
			return &rule
		}
	}

	return nil
}

func (e *ShardingEngine) hashShard(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	hash := h.Sum32()

	e.mu.RLock()
	shardCount := len(e.shards)
	e.mu.RUnlock()

	if shardCount == 0 {
		return 0
	}

	return int(hash) % shardCount
}

func (e *ShardingEngine) rangeShard(key string, rule *ShardRule) (int, error) {
	var num int
	fmt.Sscanf(key, "%d", &num)

	if num >= rule.RangeStart && num <= rule.RangeEnd {
		e.mu.RLock()
		shardCount := len(e.shards)
		e.mu.RUnlock()

		if shardCount == 0 {
			return 0, nil
		}

		shardRange := (rule.RangeEnd - rule.RangeStart + 1) / shardCount
		return (num - rule.RangeStart) / shardRange, nil
	}

	return 0, fmt.Errorf("key out of range: %s", key)
}

func (e *ShardingEngine) listShard(key string, rule *ShardRule) (int, error) {
	return 0, nil
}

func (e *ShardingEngine) AddShard(shard *Shard) {
	e.mu.Lock()
	defer e.mu.Unlock()

	shard.Healthy = true
	e.shards[shard.ID] = shard
}

func (e *ShardingEngine) RemoveShard(id int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.shards, id)
}

func (e *ShardingEngine) GetShard(id int) (*Shard, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	shard, ok := e.shards[id]
	return shard, ok
}

func (e *ShardingEngine) ListShards() []*Shard {
	e.mu.RLock()
	defer e.mu.RUnlock()

	shards := make([]*Shard, 0, len(e.shards))
	for _, s := range e.shards {
		shards = append(shards, s)
	}
	return shards
}

func (e *ShardingEngine) AddRule(rule ShardRule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.shardRules = append(e.shardRules, rule)
}

func (e *ShardingEngine) RemoveRule(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	var newRules []ShardRule
	for _, r := range e.shardRules {
		if r.Name != name {
			newRules = append(newRules, r)
		}
	}
	e.shardRules = newRules
}

func (e *ShardingEngine) GetRules() []ShardRule {
	e.mu.RLock()
	defer e.mu.RUnlock()

	rules := make([]ShardRule, len(e.shardRules))
	copy(rules, e.shardRules)
	return rules
}

func (e *ShardingEngine) SetShardHealth(id int, healthy bool) error {
	e.mu.RLock()
	shard, ok := e.shards[id]
	e.mu.RUnlock()

	if !ok {
		return fmt.Errorf("shard not found: %d", id)
	}

	shard.mu.Lock()
	shard.Healthy = healthy
	shard.mu.Unlock()

	return nil
}

func (e *ShardingEngine) GetShardStats() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["total_shards"] = len(e.shards)

	var healthyCount int
	for _, shard := range e.shards {
		shard.mu.RLock()
		if shard.Healthy {
			healthyCount++
		}
		shard.mu.RUnlock()
	}

	stats["healthy_shards"] = healthyCount
	stats["unhealthy_shards"] = len(e.shards) - healthyCount

	return stats
}

func (e *ShardingEngine) RewriteQueryForShard(qc *types.QueryContext, shardID int) string {
	query := qc.RawQuery

	if qc.Table != "" {
		query = fmt.Sprintf("%s_shard%d", qc.Table, shardID)
	}

	return query
}

func (e *ShardingEngine) SetConfig(config *ShardingConfig) {
	e.config = config
}

func (e *ShardingEngine) GetConfig() *ShardingConfig {
	return e.config
}

func (s *Shard) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"id":      s.ID,
		"name":    s.Name,
		"host":    s.Host,
		"port":    s.Port,
		"weight":  s.Weight,
		"healthy": s.Healthy,
	}
}
