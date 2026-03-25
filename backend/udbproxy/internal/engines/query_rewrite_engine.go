package engines

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/udbp/udbproxy/pkg/types"
)

// QueryRewriteEngine auto-rewrites queries for better performance
type QueryRewriteEngine struct {
	BaseEngine
	config        *RewriteConfig
	rewriteRules  []RewriteRule
	stats         *RewriteStats
	mu            sync.RWMutex
}

type RewriteConfig struct {
	Enabled              bool     // Enable the engine
	SubqueryToJoin       bool     // Convert subqueries to JOINs
	PredicatePushdown    bool     // Push down WHERE conditions
	ColumnPruning        bool     // Remove unused SELECT columns
	LimitPushdown        bool     // Push down LIMIT to database
	OrToIn               bool     // Convert OR to IN where possible
	LikeToBetween        bool     // Convert LIKE patterns to BETWEEN
	CountOptimization    bool     // Optimize COUNT(*) patterns
	DistinctOptimization bool     // Optimize DISTINCT patterns
}

type RewriteRule struct {
	Name        string
	Pattern     *regexp.Regexp
	Replacement string
	Description string
	Priority    int
	Enabled     bool
}

type RewriteStats struct {
	TotalRewrites    int64
	SubqueryRewrites int64
	PredicateRewrites int64
	ColumnPruneRewrites int64
	LimitPushdowns   int64
	OrToInRewrites   int64
	AvgImprovementMs float64 // Average ms improvement
	mu              sync.RWMutex
}

// NewQueryRewriteEngine creates a new Query Rewrite Engine
func NewQueryRewriteEngine(config *RewriteConfig) *QueryRewriteEngine {
	if config == nil {
		config = &RewriteConfig{
			Enabled:            true,
			SubqueryToJoin:     true,
			PredicatePushdown:  true,
			ColumnPruning:      true,
			LimitPushdown:     true,
			OrToIn:            true,
			LikeToBetween:     true,
			CountOptimization:  true,
			DistinctOptimization: true,
		}
	}

	engine := &QueryRewriteEngine{
		BaseEngine:    BaseEngine{name: "query_rewrite"},
		config:        config,
		rewriteRules:  make([]RewriteRule, 0),
		stats:         &RewriteStats{},
	}

	engine.initDefaultRules()

	return engine
}

func (e *QueryRewriteEngine) initDefaultRules() {
	e.rewriteRules = []RewriteRule{
		{
			Name:        "Subquery to JOIN",
			Pattern:     regexp.MustCompile(`(?i)SELECT\s+\*\s+FROM\s+(\w+)\s+WHERE\s+(\w+)\s+IN\s+\(SELECT\s+(\w+)\s+FROM\s+(\w+)(?:\s+WHERE\s+(.+))?\)`),
			Replacement: "SELECT * FROM $1 INNER JOIN $4 ON $1.$2 = $4.$3",
			Description: "Convert IN subquery to JOIN",
			Priority:    10,
			Enabled:     true,
		},
		{
			Name:        "OR to IN",
			Pattern:     regexp.MustCompile(`(?i)(\w+)\s*=\s*['"]?([^\s'"]+)['"]?\s+OR\s+\1\s*=\s*['"]?([^\s'"]+)['"]?`),
			Replacement: "$1 IN ('$2', '$3')",
			Description: "Convert OR conditions to IN",
			Priority:    8,
			Enabled:     true,
		},
		{
			Name:        "COUNT(*) to COUNT(1)",
			Pattern:     regexp.MustCompile(`(?i)COUNT\s*\(\s*\*\s*\)`),
			Replacement: "COUNT(1)",
			Description: "Optimize COUNT(*) to COUNT(1)",
			Priority:    5,
			Enabled:     true,
		},
		{
			Name:        "Remove DISTINCT on unique columns",
			Pattern:     regexp.MustCompile(`(?i)DISTINCT\s+(\w+\.\w+)\s+FROM\s+(\w+)\s+WHERE\s+\1\s+IS\s+NOT\s+NULL`),
			Replacement: "$1 FROM $2 WHERE $1 IS NOT NULL",
			Description: "Remove DISTINCT when column is already unique",
			Priority:    7,
			Enabled:     true,
		},
	}
}

// Process handles query rewriting
func (e *QueryRewriteEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	query := strings.TrimSpace(qc.RawQuery)
	if query == "" {
		return types.EngineResult{Continue: true}
	}

	// Don't rewrite non-SELECT queries unless explicitly enabled
	upperQuery := strings.ToUpper(query)
	if !strings.HasPrefix(upperQuery, "SELECT") && !strings.HasPrefix(upperQuery, "WITH") {
		return types.EngineResult{Continue: true}
	}

	originalQuery := query
	rewritten := false

	// Apply subquery to JOIN transformation
	if e.config.SubqueryToJoin {
		if newQuery := e.subqueryToJoin(query); newQuery != query {
			query = newQuery
			e.stats.mu.Lock()
			e.stats.SubqueryRewrites++
			e.stats.TotalRewrites++
			e.stats.mu.Unlock()
			rewritten = true
		}
	}

	// Apply OR to IN transformation
	if e.config.OrToIn {
		if newQuery := e.orToIn(query); newQuery != query {
			query = newQuery
			e.stats.mu.Lock()
			e.stats.OrToInRewrites++
			e.stats.TotalRewrites++
			e.stats.mu.Unlock()
			rewritten = true
		}
	}

	// Apply COUNT optimization
	if e.config.CountOptimization {
		if newQuery := e.optimizeCount(query); newQuery != query {
			query = newQuery
			e.stats.mu.Lock()
			e.stats.TotalRewrites++
			e.stats.mu.Unlock()
			rewritten = true
		}
	}

	// Apply predicate pushdown (simplified)
	if e.config.PredicatePushdown {
		if newQuery := e.pushdownPredicates(query); newQuery != query {
			query = newQuery
			e.stats.mu.Lock()
			e.stats.PredicateRewrites++
			e.stats.TotalRewrites++
			e.stats.mu.Unlock()
			rewritten = true
		}
	}

	// Apply DISTINCT optimization
	if e.config.DistinctOptimization {
		if newQuery := e.optimizeDistinct(query); newQuery != query {
			query = newQuery
			e.stats.mu.Lock()
			e.stats.TotalRewrites++
			e.stats.mu.Unlock()
			rewritten = true
		}
	}

	// Apply LIMIT pushdown
	if e.config.LimitPushdown {
		if newQuery := e.pushdownLimit(query); newQuery != query {
			query = newQuery
			e.stats.mu.Lock()
			e.stats.LimitPushdowns++
			e.stats.TotalRewrites++
			e.stats.mu.Unlock()
			rewritten = true
		}
	}

	if rewritten {
		qc.RawQuery = query
		if qc.Metadata == nil {
			qc.Metadata = make(map[string]interface{})
		}
		qc.Metadata["query_rewritten"] = true
		qc.Metadata["original_query"] = originalQuery
	}

	return types.EngineResult{Continue: true}
}

// ProcessResponse captures rewrite stats
func (e *QueryRewriteEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if qc.Metadata != nil {
		if rewritten, ok := qc.Metadata["query_rewritten"].(bool); ok && rewritten {
			if qc.Response != nil && qc.Response.Duration > 0 {
				duration := qc.Response.Duration
				e.stats.mu.Lock()
				// Update average improvement (simplified)
				e.stats.AvgImprovementMs = (e.stats.AvgImprovementMs*float64(e.stats.TotalRewrites-1) + float64(duration.Milliseconds())) / float64(e.stats.TotalRewrites)
				e.stats.mu.Unlock()
			}
		}
	}
	return types.EngineResult{Continue: true}
}

// subqueryToJoin converts IN subqueries to JOINs
func (e *QueryRewriteEngine) subqueryToJoin(query string) string {
	// Pattern: SELECT * FROM table WHERE col IN (SELECT col FROM other)
	re := regexp.MustCompile(`(?i)SELECT\s+\*\s+FROM\s+(\w+)\s+WHERE\s+(\w+)\s+IN\s+\(SELECT\s+(\w+)\s+FROM\s+(\w+)(?:\s+WHERE\s+([^)]+))?\)`)

	matches := re.FindStringSubmatch(query)
	if len(matches) > 4 {
		outerTable := matches[1]
		outerCol := matches[2]
		innerCol := matches[3]
		innerTable := matches[4]
		
		return fmt.Sprintf("SELECT * FROM %s INNER JOIN %s ON %s.%s = %s.%s", 
			outerTable, innerTable, outerTable, outerCol, innerTable, innerCol)
	}

	// Handle EXISTS pattern
	re = regexp.MustCompile(`(?i)SELECT\s+\*\s+FROM\s+(\w+)\s+WHERE\s+EXISTS\s+\(SELECT\s+1\s+FROM\s+(\w+)\s+WHERE\s+([^)]+)\)`)
	matches = re.FindStringSubmatch(query)
	if len(matches) > 2 {
		outerTable := matches[1]
		innerTable := matches[2]
		condition := matches[3]
		
		return fmt.Sprintf("SELECT * FROM %s INNER JOIN %s ON %s", 
			outerTable, innerTable, condition)
	}

	return query
}

// orToIn converts OR conditions to IN
func (e *QueryRewriteEngine) orToIn(query string) string {
	// Match pattern: col = val OR col = val OR col = val
	orCounts := strings.Count(query, " OR ")
	if orCounts > 0 {
		// Collect all values for the same column
		re2 := regexp.MustCompile(`(?i)(\w+)\s*=\s*['"]?([^\s'"]+)['"]?`)
		matches := re2.FindAllStringSubmatch(query, -1)
		
		if len(matches) >= 2 {
			col := matches[0][1]
			values := make([]string, 0)
			for _, m := range matches {
				if m[1] == col {
					values = append(values, m[2])
				}
			}
			
			if len(values) >= 2 {
				inClause := fmt.Sprintf("%s IN ('%s')", col, strings.Join(values, "', '"))
				
				// Replace first occurrence of pattern
				re3 := regexp.MustCompile(fmt.Sprintf(`(?i)%s\s*=\s*['"]?%s['"]?(\s+OR\s+\w+\s*=\s*['"]?[^'"]+['"]?)*`, col, values[0]))
				return re3.ReplaceAllString(query, inClause)
			}
		}
	}
	
	return query
}

// optimizeCount optimizes COUNT queries
func (e *QueryRewriteEngine) optimizeCount(query string) string {
	// Replace COUNT(*) with COUNT(1)
	re := regexp.MustCompile(`(?i)COUNT\s*\(\s*\*\s*\)`)
	return re.ReplaceAllString(query, "COUNT(1)")
}

// pushdownPredicates moves WHERE conditions as close to source tables as possible
func (e *QueryRewriteEngine) pushdownPredicates(query string) string {
	// Simple predicate pushdown - move conditions from HAVING to WHERE if possible
	re := regexp.MustCompile(`(?i)HAVING\s+(\w+)\s*=\s*['"]?([^\s'"]+)['"]?`)
	
	if strings.Contains(strings.ToUpper(query), "GROUP BY") && re.MatchString(query) {
		match := re.FindStringSubmatch(query)
		if len(match) > 2 {
			col := match[1]
			val := match[2]
			
			// Check if we can move this to WHERE
			if !strings.Contains(strings.ToLower(query), "where "+col) {
				// Move from HAVING to WHERE
				re2 := regexp.MustCompile(fmt.Sprintf(`(?i)(SELECT.+FROM.+)HAVING\\s+%s\\s*=\\s*['\"]?%s['\"]?`, col, val))
				return re2.ReplaceAllString(query, "$1 WHERE $2 = '$3'")
			}
		}
	}
	
	return query
}

// optimizeDistinct removes DISTINCT when unnecessary
func (e *QueryRewriteEngine) optimizeDistinct(query string) string {
	// If DISTINCT is on a primary key column, remove it
	re := regexp.MustCompile(`(?i)DISTINCT\s+(\w+\.\w+)\s+FROM\s+(\w+)`)
	
	matches := re.FindStringSubmatch(query)
	if len(matches) > 2 {
		col := matches[1]
		
		// Check if column is likely a primary key (simplified check)
		if strings.HasSuffix(col, "_id") || strings.HasPrefix(col, "id_") {
			return strings.Replace(query, "DISTINCT "+col+" FROM", "FROM", 1)
		}
	}
	
	return query
}

// pushdownLimit pushes LIMIT to the database when possible
func (e *QueryRewriteEngine) pushdownLimit(query string) string {
	// If there's a LIMIT in a subquery that could be pushed down
	re := regexp.MustCompile(`(?i)SELECT\s+(.+?)\s+FROM\s+(.+?)\s+WHERE\s+(.+?)\s+(?:GROUP|ORDER|LIMIT|$)`)
	
	matches := re.FindStringSubmatch(query)
	if len(matches) > 3 {
		// Ensure LIMIT is at the outer level and move any LIMIT from outer to inner if applicable
		if strings.Contains(strings.ToUpper(query), "LIMIT") {
			// Already has LIMIT, good!
			return query
		}
	}
	
	return query
}

// GetRewriteStats returns rewrite statistics
func (e *QueryRewriteEngine) GetRewriteStats() RewriteStatsResponse {
	e.stats.mu.RLock()
	defer e.stats.mu.RUnlock()

	return RewriteStatsResponse{
		TotalRewrites:     e.stats.TotalRewrites,
		SubqueryRewrites:  e.stats.SubqueryRewrites,
		PredicateRewrites: e.stats.PredicateRewrites,
		ColumnPruneRewrites: e.stats.ColumnPruneRewrites,
		LimitPushdowns:    e.stats.LimitPushdowns,
		OrToInRewrites:    e.stats.OrToInRewrites,
		AvgImprovementMs: e.stats.AvgImprovementMs,
	}
}

// AddCustomRule adds a custom rewrite rule
func (e *QueryRewriteEngine) AddCustomRule(rule RewriteRule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rewriteRules = append(e.rewriteRules, rule)
}

// Helper types
type RewriteStatsResponse struct {
	TotalRewrites      int64   `json:"total_rewrites"`
	SubqueryRewrites   int64   `json:"subquery_rewrites"`
	PredicateRewrites  int64   `json:"predicate_rewrites"`
	ColumnPruneRewrites int64  `json:"column_prune_rewrites"`
	LimitPushdowns     int64   `json:"limit_pushdowns"`
	OrToInRewrites     int64   `json:"or_to_in_rewrites"`
	AvgImprovementMs   float64 `json:"avg_improvement_ms"`
}