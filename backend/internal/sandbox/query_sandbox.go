package sandbox

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

type QuerySandbox struct {
	mu     sync.RWMutex
	config *SandboxConfig
	rules  []SandboxRule
	stats  *SandboxStats
}

type SandboxConfig struct {
	Enabled         bool
	MaxQueryLength  int
	MaxResultRows   int
	Timeout         time.Duration
	BlockPatterns   []string
	AllowedPatterns []string
}

type SandboxRule struct {
	Name        string
	Pattern     string
	Action      SandboxAction
	Severity    string
	Description string
}

type SandboxAction string

const (
	SandboxActionAllow SandboxAction = "allow"
	SandboxActionBlock SandboxAction = "block"
	SandboxActionWarn  SandboxAction = "warn"
	SandboxActionMask  SandboxAction = "mask"
)

type SandboxStats struct {
	TotalQueries   int64
	BlockedQueries int64
	WarnedQueries  int64
	AllowedQueries int64
	AvgProcessTime time.Duration
	mu             sync.Mutex
}

func NewQuerySandbox(config *SandboxConfig) *QuerySandbox {
	if config == nil {
		config = &SandboxConfig{
			Enabled:        true,
			MaxQueryLength: 10000,
			MaxResultRows:  1000,
			Timeout:        30 * time.Second,
		}
	}

	return &QuerySandbox{
		config: config,
		rules:  []SandboxRule{},
		stats:  &SandboxStats{},
	}
}

func (qs *QuerySandbox) ValidateQuery(query string) *SandboxResult {
	startTime := time.Now()
	defer func() {
		qs.updateStats(time.Since(startTime))
	}()

	result := &SandboxResult{
		Allowed: true,
		Query:   query,
	}

	if !qs.config.Enabled {
		return result
	}

	if len(query) > qs.config.MaxQueryLength {
		result.Allowed = false
		result.Action = SandboxActionBlock
		result.Error = fmt.Errorf("query exceeds max length: %d > %d", len(query), qs.config.MaxQueryLength)
		qs.stats.mu.Lock()
		qs.stats.BlockedQueries++
		qs.stats.TotalQueries++
		qs.stats.mu.Unlock()
		return result
	}

	if qs.config.AllowedPatterns != nil && len(qs.config.AllowedPatterns) > 0 {
		allowed := false
		for _, pattern := range qs.config.AllowedPatterns {
			if match, _ := regexp.MatchString(pattern, query); match {
				allowed = true
				break
			}
		}
		if !allowed {
			result.Allowed = false
			result.Action = SandboxActionBlock
			result.Error = fmt.Errorf("query does not match allowed patterns")
			qs.stats.mu.Lock()
			qs.stats.BlockedQueries++
			qs.stats.TotalQueries++
			qs.stats.mu.Unlock()
			return result
		}
	}

	for _, pattern := range qs.config.BlockPatterns {
		if match, _ := regexp.MatchString(pattern, query); match {
			result.Allowed = false
			result.Action = SandboxActionBlock
			result.Error = fmt.Errorf("query matches blocked pattern: %s", pattern)
			qs.stats.mu.Lock()
			qs.stats.BlockedQueries++
			qs.stats.TotalQueries++
			qs.stats.mu.Unlock()
			return result
		}
	}

	qs.mu.RLock()
	defer qs.mu.RUnlock()

	for _, rule := range qs.rules {
		if match, _ := regexp.MatchString(rule.Pattern, query); match {
			switch rule.Action {
			case SandboxActionBlock:
				result.Allowed = false
				result.Action = SandboxActionBlock
				result.Error = fmt.Errorf("query blocked by rule: %s", rule.Name)
				qs.stats.mu.Lock()
				qs.stats.BlockedQueries++
				qs.stats.TotalQueries++
				qs.stats.mu.Unlock()
				return result
			case SandboxActionWarn:
				result.Allowed = true
				result.Action = SandboxActionWarn
				result.Warnings = append(result.Warnings, fmt.Sprintf("query matches warning rule: %s", rule.Name))
				qs.stats.mu.Lock()
				qs.stats.WarnedQueries++
				qs.stats.TotalQueries++
				qs.stats.mu.Unlock()
			case SandboxActionMask:
				result.Allowed = true
				result.Action = SandboxActionMask
				result.MaskedQuery = qs.maskQuery(query, rule.Pattern)
			}
		}
	}

	if result.Allowed && result.Action == "" {
		qs.stats.mu.Lock()
		qs.stats.AllowedQueries++
		qs.stats.TotalQueries++
		qs.stats.mu.Unlock()
	}

	return result
}

func (qs *QuerySandbox) ValidateResult(rows int) *SandboxResult {
	result := &SandboxResult{Allowed: true}

	if rows > qs.config.MaxResultRows {
		result.Allowed = false
		result.Action = SandboxActionBlock
		result.Error = fmt.Errorf("result exceeds max rows: %d > %d", rows, qs.config.MaxResultRows)
	}

	return result
}

func (qs *QuerySandbox) AddRule(rule SandboxRule) {
	qs.mu.Lock()
	defer qs.mu.Unlock()
	qs.rules = append(qs.rules, rule)
}

func (qs *QuerySandbox) RemoveRule(name string) {
	qs.mu.Lock()
	defer qs.mu.Unlock()

	var newRules []SandboxRule
	for _, r := range qs.rules {
		if r.Name != name {
			newRules = append(newRules, r)
		}
	}
	qs.rules = newRules
}

func (qs *QuerySandbox) GetRules() []SandboxRule {
	qs.mu.RLock()
	defer qs.mu.RUnlock()

	rules := make([]SandboxRule, len(qs.rules))
	copy(rules, qs.rules)
	return rules
}

func (qs *QuerySandbox) GetStats() *SandboxStats {
	qs.stats.mu.Lock()
	defer qs.stats.mu.Unlock()

	return &SandboxStats{
		TotalQueries:   qs.stats.TotalQueries,
		BlockedQueries: qs.stats.BlockedQueries,
		WarnedQueries:  qs.stats.WarnedQueries,
		AllowedQueries: qs.stats.AllowedQueries,
		AvgProcessTime: qs.stats.AvgProcessTime,
	}
}

func (qs *QuerySandbox) ResetStats() {
	qs.stats.mu.Lock()
	defer qs.stats.mu.Unlock()
	qs.stats.TotalQueries = 0
	qs.stats.BlockedQueries = 0
	qs.stats.WarnedQueries = 0
	qs.stats.AllowedQueries = 0
	qs.stats.AvgProcessTime = 0
}

func (qs *QuerySandbox) SetConfig(config *SandboxConfig) {
	qs.mu.Lock()
	defer qs.mu.Unlock()
	qs.config = config
}

func (qs *QuerySandbox) GetConfig() *SandboxConfig {
	qs.mu.RLock()
	defer qs.mu.RUnlock()
	return qs.config
}

func (qs *QuerySandbox) updateStats(duration time.Duration) {
	qs.stats.mu.Lock()
	defer qs.stats.mu.Unlock()

	if qs.stats.TotalQueries > 0 {
		totalDuration := qs.stats.AvgProcessTime.Nanoseconds() * (qs.stats.TotalQueries - 1)
		qs.stats.AvgProcessTime = time.Duration((totalDuration + duration.Nanoseconds()) / qs.stats.TotalQueries)
	} else {
		qs.stats.AvgProcessTime = duration
	}
}

func (qs *QuerySandbox) maskQuery(query, pattern string) string {
	re := regexp.MustCompile("(?i)" + pattern)
	return re.ReplaceAllString(query, "***")
}

type SandboxResult struct {
	Allowed     bool
	Query       string
	MaskedQuery string
	Action      SandboxAction
	Error       error
	Warnings    []string
	Duration    time.Duration
}

func (sr *SandboxResult) String() string {
	if !sr.Allowed {
		return fmt.Sprintf("BLOCKED: %s", sr.Error)
	}
	if len(sr.Warnings) > 0 {
		return fmt.Sprintf("WARN: %s", strings.Join(sr.Warnings, ", "))
	}
	return "ALLOWED"
}
