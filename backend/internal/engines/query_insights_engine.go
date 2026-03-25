package engines

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/udbp/udbproxy/pkg/types"
)

// QueryInsightsEngine provides deep query analysis, pattern recognition, and performance insights
type QueryInsightsEngine struct {
	BaseEngine
	config        *QueryInsightsConfig
	queryPatterns *QueryPatternStore
	slowQueryLog  *SlowQueryLog
	indexRecs     *IndexRecommendations
	mu            sync.RWMutex
}

type QueryInsightsConfig struct {
	Enabled           bool          // Enable the engine
	SlowQueryThreshold time.Duration // Threshold for slow queries (default: 1s)
	PatternWindow     time.Duration // Time window for pattern analysis
	MaxPatterns       int           // Maximum patterns to track
	IndexRecEnabled   bool          // Enable index recommendations
	ComplexityEnabled bool          // Enable query complexity scoring
}

type QueryPatternStore struct {
	patterns map[string]*QueryPattern
	mu       sync.RWMutex
}

type QueryPattern struct {
	QueryTemplate  string
	Hash           string
	Count          int64
	AvgDuration    time.Duration
	TotalDuration  time.Duration
	MinDuration    time.Duration
	MaxDuration    time.Duration
	LastSeen       time.Time
	FirstSeen      time.Time
	TableAccess    []string
	ColumnsUsed    []string
	IsWrite        bool
	Complexity     int // 1-10 complexity score
}

type SlowQueryLog struct {
	queries  []SlowQueryEntry
	maxSize  int
	mu       sync.RWMutex
}

type SlowQueryEntry struct {
	Query        string
	Duration     time.Duration
	Timestamp    time.Time
	Tables       []string
	Complexity   int
	Recommendations []string
}

type IndexRecommendations struct {
	recommendations map[string][]IndexRecommendation
	mu             sync.RWMutex
}

type IndexRecommendation struct {
	Table       string
	Columns     []string
	IndexType   string // "B-TREE", "HASH", "GIN", etc.
	Confidence  float64
	Reason      string
	Impact      string // "HIGH", "MEDIUM", "LOW"
}

// NewQueryInsightsEngine creates a new Query Insights Engine
func NewQueryInsightsEngine(config *QueryInsightsConfig) *QueryInsightsEngine {
	if config == nil {
		config = &QueryInsightsConfig{
			Enabled:            true,
			SlowQueryThreshold: 1000 * time.Millisecond,
			PatternWindow:      10 * time.Minute,
			MaxPatterns:        1000,
			IndexRecEnabled:    true,
			ComplexityEnabled:  true,
		}
	}

	engine := &QueryInsightsEngine{
		BaseEngine:    BaseEngine{name: "query_insights"},
		config:       config,
		queryPatterns: &QueryPatternStore{patterns: make(map[string]*QueryPattern)},
		slowQueryLog:  &SlowQueryLog{queries: make([]SlowQueryEntry, 0), maxSize: 1000},
		indexRecs:     &IndexRecommendations{recommendations: make(map[string][]IndexRecommendation)},
	}

	// Start background cleanup
	go engine.cleanupLoop()

	return engine
}

// Process analyzes the query and extracts insights
func (e *QueryInsightsEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	query := strings.TrimSpace(qc.RawQuery)
	if query == "" {
		return types.EngineResult{Continue: true}
	}

	// Normalize query for pattern matching
	template := e.normalizeQuery(query)
	hash := e.hashQuery(template)

	e.mu.Lock()
	defer e.mu.Unlock()

	// Extract tables and columns from query
	tables := e.extractTables(query)
	columns := e.extractColumns(query)
	isWrite := e.isWriteQuery(query)
	complexity := e.calculateComplexity(query)

	// Update pattern store
	if pattern, exists := e.queryPatterns.patterns[hash]; exists {
		pattern.Count++
		pattern.LastSeen = time.Now()
		// Update duration statistics
		var duration time.Duration
		if qc.Response != nil {
			duration = qc.Response.Duration
		}
		pattern.TotalDuration += duration
		pattern.AvgDuration = pattern.TotalDuration / time.Duration(pattern.Count)
		if duration > pattern.MaxDuration {
			pattern.MaxDuration = duration
		}
		if duration < pattern.MinDuration || pattern.MinDuration == 0 {
			pattern.MinDuration = duration
		}
	} else {
		var duration time.Duration
		if qc.Response != nil {
			duration = qc.Response.Duration
		}
		e.queryPatterns.patterns[hash] = &QueryPattern{
			QueryTemplate: template,
			Hash:          hash,
			Count:         1,
			AvgDuration:   duration,
			MinDuration:   duration,
			MaxDuration:   duration,
			TotalDuration: duration,
			LastSeen:      time.Now(),
			FirstSeen:     time.Now(),
			TableAccess:   tables,
			ColumnsUsed:   columns,
			IsWrite:       isWrite,
			Complexity:    complexity,
		}
	}

	// Check for slow query
	var duration time.Duration
	if qc.Response != nil {
		duration = qc.Response.Duration
	}
	if duration > e.config.SlowQueryThreshold {
		e.recordSlowQuery(query, duration, tables, complexity)
	}

	// Generate index recommendations
	if e.config.IndexRecEnabled && !isWrite {
		e.generateIndexRecommendations(tables, query)
	}

	// Add insights to metadata
	if qc.Metadata == nil {
		qc.Metadata = make(map[string]interface{})
	}
	qc.Metadata["query_complexity"] = complexity
	qc.Metadata["query_tables"] = tables
	qc.Metadata["is_write"] = isWrite

	return types.EngineResult{Continue: true}
}

// ProcessResponse captures query execution results
func (e *QueryInsightsEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	// Update metadata with insights
	if qc.Metadata == nil {
		qc.Metadata = make(map[string]interface{})
	}

	e.mu.RLock()
	hash := e.hashQuery(e.normalizeQuery(qc.RawQuery))
	if pattern, exists := e.queryPatterns.patterns[hash]; exists {
		qc.Metadata["pattern_count"] = pattern.Count
		qc.Metadata["pattern_avg_duration"] = pattern.AvgDuration.String()
	}
	e.mu.RUnlock()

	return types.EngineResult{Continue: true}
}

// normalizeQuery converts query to a template by replacing literals
func (e *QueryInsightsEngine) normalizeQuery(query string) string {
	// Replace strings
	re := regexp.MustCompile(`'[^']*'`)
	query = re.ReplaceAllString(query, "'?'")

	// Replace numbers
	re = regexp.MustCompile(`\b\d+\b`)
	query = re.ReplaceAllString(query, "?")

	// Normalize whitespace
	query = strings.Join(strings.Fields(query), " ")

	return query
}

// hashQuery creates a hash for the query template
func (e *QueryInsightsEngine) hashQuery(template string) string {
	hash := 0
	for _, c := range template {
		hash = hash*31 + int(c)
	}
	return fmt.Sprintf("%x", hash)
}

// extractTables extracts table names from query
func (e *QueryInsightsEngine) extractTables(query string) []string {
	// Common table extraction patterns
	patterns := []string{
		`(?i)FROM\s+(\w+)`,
		`(?i)JOIN\s+(\w+)`,
		`(?i)INTO\s+(\w+)`,
		`(?i)UPDATE\s+(\w+)`,
		`(?i)TABLE\s+(\w+)`,
	}

	tables := make(map[string]bool)
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindAllStringSubmatch(query, -1)
		for _, match := range matches {
			if len(match) > 1 {
				tables[strings.ToLower(match[1])] = true
			}
		}
	}

	result := make([]string, 0, len(tables))
	for t := range tables {
		result = append(result, t)
	}
	return result
}

// extractColumns extracts column names from query
func (e *QueryInsightsEngine) extractColumns(query string) []string {
	re := regexp.MustCompile(`(?i)(?:SELECT|WHERE|SET|VALUES)\s+([\w,\s]+)`)
	matches := re.FindAllStringSubmatch(query, -1)

	columns := make(map[string]bool)
	for _, match := range matches {
		if len(match) > 1 {
			parts := strings.Split(match[1], ",")
			for _, p := range parts {
				col := strings.TrimSpace(strings.Fields(p)[0])
				if col != "" && !isSQLKeyword(col) {
					columns[strings.ToLower(col)] = true
				}
			}
		}
	}

	result := make([]string, 0, len(columns))
	for c := range columns {
		result = append(result, c)
	}
	return result
}

// isWriteQuery determines if query is a write operation
func (e *QueryInsightsEngine) isWriteQuery(query string) bool {
	writeKeywords := []string{"INSERT", "UPDATE", "DELETE", "CREATE", "DROP", "ALTER", "TRUNCATE"}
	upper := strings.ToUpper(query)
	for _, kw := range writeKeywords {
		if strings.Contains(upper, kw) {
			return true
		}
	}
	return false
}

// calculateComplexity scores query complexity (1-10)
func (e *QueryInsightsEngine) calculateComplexity(query string) int {
	score := 1

	// Count joins
	joinCount := len(regexp.MustCompile(`(?i)\bJOIN\b`).FindAllStringIndex(query, -1))
	score += joinCount * 2

	// Count subqueries
	subqueryCount := len(regexp.MustCompile(`(?i)\bSELECT\b`).FindAllStringIndex(query, -1)) - 1
	score += subqueryCount * 2

	// Count WHERE conditions
	whereCount := len(regexp.MustCompile(`(?i)\bWHERE\b`).FindAllStringIndex(query, -1))
	if whereCount > 0 {
		andCount := len(regexp.MustCompile(`(?i)\bAND\b`).FindAllStringIndex(query, -1))
		score += andCount
	}

	// Count aggregations
	aggCount := len(regexp.MustCompile(`(?i)\b(COUNT|SUM|AVG|MIN|MAX|GROUP BY|ORDER BY)\b`).FindAllStringIndex(query, -1))
	score += aggCount

	// Count LIKE/REGEX
	likeCount := len(regexp.MustCompile(`(?i)\b(LIKE|REGEX|RLIKE)\b`).FindAllStringIndex(query, -1))
	score += likeCount * 2

	// Cap at 10
	if score > 10 {
		score = 10
	}

	return score
}

// recordSlowQuery logs a slow query for analysis
func (e *QueryInsightsEngine) recordSlowQuery(query string, duration time.Duration, tables []string, complexity int) {
	entry := SlowQueryEntry{
		Query:      query,
		Duration:  duration,
		Timestamp: time.Now(),
		Tables:    tables,
		Complexity: complexity,
		Recommendations: e.generateRecommendations(query, duration, complexity),
	}

	e.slowQueryLog.mu.Lock()
	e.slowQueryLog.queries = append(e.slowQueryLog.queries, entry)
	// Keep only last maxSize entries
	if len(e.slowQueryLog.queries) > e.slowQueryLog.maxSize {
		e.slowQueryLog.queries = e.slowQueryLog.queries[1:]
	}
	e.slowQueryLog.mu.Unlock()
}

// generateRecommendations generates suggestions for slow queries
func (e *QueryInsightsEngine) generateRecommendations(query string, duration time.Duration, complexity int) []string {
	recs := make([]string, 0)

	if complexity > 7 {
		recs = append(recs, "Consider simplifying query complexity")
	}

	if strings.Contains(strings.ToUpper(query), "SELECT *") {
		recs = append(recs, "Avoid SELECT *, specify needed columns")
	}

	if strings.Contains(strings.ToUpper(query), "LIKE '%") {
		recs = append(recs, "Leading wildcard prevents index usage")
	}

	if strings.Contains(strings.ToUpper(query), "ORDER BY RAND()") {
		recs = append(recs, "ORDER BY RAND() is expensive, consider alternative")
	}

	return recs
}

// generateIndexRecommendations analyzes queries and suggests indexes
func (e *QueryInsightsEngine) generateIndexRecommendations(tables []string, query string) {
	for _, table := range tables {
		// Analyze WHERE clause columns
		re := regexp.MustCompile(`(?i)WHERE\s+(\w+)\s*=`)
		matches := re.FindAllStringSubmatch(query, -1)

		for _, match := range matches {
			if len(match) > 1 {
				col := match[1]
				
				rec := IndexRecommendation{
					Table:      table,
					Columns:    []string{col},
					IndexType:  "B-TREE",
					Confidence: 0.8,
					Reason:     "Frequently used in WHERE clause",
					Impact:     "HIGH",
				}

				e.indexRecs.mu.Lock()
				if _, exists := e.indexRecs.recommendations[table]; !exists {
					e.indexRecs.recommendations[table] = make([]IndexRecommendation, 0)
				}
				
				// Check if similar recommendation exists
				found := false
				for i, existing := range e.indexRecs.recommendations[table] {
					if existing.Columns[0] == col {
						e.indexRecs.recommendations[table][i].Confidence = math.Min(1.0, existing.Confidence+0.1)
						found = true
						break
					}
				}
				if !found {
					e.indexRecs.recommendations[table] = append(e.indexRecs.recommendations[table], rec)
				}
				e.indexRecs.mu.Unlock()
			}
		}
	}
}

// cleanupLoop periodically cleans up old patterns
func (e *QueryInsightsEngine) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		e.mu.Lock()
		// Remove patterns not seen in last hour
		cutoff := time.Now().Add(-1 * time.Hour)
		for hash, pattern := range e.queryPatterns.patterns {
			if pattern.LastSeen.Before(cutoff) {
				delete(e.queryPatterns.patterns, hash)
			}
		}
		// Enforce max patterns
		if len(e.queryPatterns.patterns) > e.config.MaxPatterns {
			// Remove oldest patterns
			patterns := make([]*QueryPattern, 0, len(e.queryPatterns.patterns))
			for _, p := range e.queryPatterns.patterns {
				patterns = append(patterns, p)
			}
			sort.Slice(patterns, func(i, j int) bool {
				return patterns[i].LastSeen.Before(patterns[j].LastSeen)
			})
			toRemove := len(patterns) - e.config.MaxPatterns
			for i := 0; i < toRemove; i++ {
				delete(e.queryPatterns.patterns, patterns[i].Hash)
			}
		}
		e.mu.Unlock()
	}
}

// GetSlowQueries returns recent slow queries
func (e *QueryInsightsEngine) GetSlowQueries(limit int) []SlowQueryLogEntry {
	e.slowQueryLog.mu.RLock()
	defer e.slowQueryLog.mu.RUnlock()

	result := make([]SlowQueryLogEntry, 0, limit)
	start := len(e.slowQueryLog.queries) - limit
	if start < 0 {
		start = 0
	}

	for i := start; i < len(e.slowQueryLog.queries); i++ {
		result = append(result, SlowQueryLogEntry{
			Query:           e.slowQueryLog.queries[i].Query,
			Duration:        e.slowQueryLog.queries[i].Duration.String(),
			Timestamp:       e.slowQueryLog.queries[i].Timestamp,
			Tables:          e.slowQueryLog.queries[i].Tables,
			Recommendations: e.slowQueryLog.queries[i].Recommendations,
		})
	}
	return result
}

// GetIndexRecommendations returns index recommendations for a table
func (e *QueryInsightsEngine) GetIndexRecommendations(table string) []IndexRecommendation {
	e.indexRecs.mu.RLock()
	defer e.indexRecs.mu.RUnlock()
	return e.indexRecs.recommendations[table]
}

// GetTopQueries returns top N queries by total duration
func (e *QueryInsightsEngine) GetTopQueries(limit int) []QueryPatternEntry {
	e.mu.RLock()
	defer e.mu.RUnlock()

	patterns := make([]*QueryPattern, 0, len(e.queryPatterns.patterns))
	for _, p := range e.queryPatterns.patterns {
		patterns = append(patterns, p)
	}

	sort.Slice(patterns, func(i, j int) bool {
		return patterns[i].TotalDuration > patterns[j].TotalDuration
	})

	result := make([]QueryPatternEntry, 0, limit)
	for i := 0; i < limit && i < len(patterns); i++ {
		result = append(result, QueryPatternEntry{
			Template:     patterns[i].QueryTemplate,
			Count:        patterns[i].Count,
			AvgDuration:  patterns[i].AvgDuration.String(),
			TotalDuration: patterns[i].TotalDuration.String(),
			Complexity:   patterns[i].Complexity,
		})
	}
	return result
}

// Helper types for API responses
type SlowQueryLogEntry struct {
	Query           string   `json:"query"`
	Duration        string   `json:"duration"`
	Timestamp       time.Time `json:"timestamp"`
	Tables          []string `json:"tables"`
	Recommendations []string `json:"recommendations"`
}

type QueryPatternEntry struct {
	Template      string `json:"template"`
	Count         int64  `json:"count"`
	AvgDuration   string `json:"avg_duration"`
	TotalDuration string `json:"total_duration"`
	Complexity    int    `json:"complexity"`
}

// isSQLKeyword checks if a string is an SQL keyword
func isSQLKeyword(word string) bool {
	keywords := map[string]bool{
		"SELECT": true, "FROM": true, "WHERE": true, "JOIN": true,
		"LEFT": true, "RIGHT": true, "INNER": true, "OUTER": true,
		"ON": true, "AND": true, "OR": true, "NOT": true, "IN": true,
		"IS": true, "NULL": true, "TRUE": false, "FALSE": false,
		"ORDER": true, "BY": true, "GROUP": true, "HAVING": true,
		"LIMIT": true, "OFFSET": true, "AS": true, "DISTINCT": true,
		"INSERT": true, "INTO": true, "VALUES": true, "UPDATE": true,
		"SET": true, "DELETE": true, "CREATE": true, "ALTER": true,
		"DROP": true, "TABLE": true, "INDEX": true, "VIEW": true,
		"CASE": true, "WHEN": true, "THEN": true, "ELSE": true,
		"END": true, "UNION": true, "ALL": true, "EXISTS": true,
		"BETWEEN": true, "LIKE": true, "ESCAPE": true,
	}
	return keywords[strings.ToUpper(word)]
}