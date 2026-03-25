package engines

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/udbp/udbproxy/pkg/logger"
	"github.com/udbp/udbproxy/pkg/types"
)

type AuditEngine struct {
	BaseEngine
	mu              sync.RWMutex
	config          *types.AuditConfig
	records         []types.AuditRecord
	piiPatterns     []*regexp.Regexp
	complianceRules []types.ComplianceRule
	maxRecords      int
}

var (
	piiEmail      = regexp.MustCompile(`(?i)[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	piiSSN        = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
	piiCreditCard = regexp.MustCompile(`\b\d{4}[-\s]?\d{4}[-\s]?\d{4}[-\s]?\d{4}\b`)
	piiPhone      = regexp.MustCompile(`\b\d{3}[-.]?\d{3}[-.]?\d{4}\b`)
)

func NewAuditEngine(config *types.AuditConfig) *AuditEngine {
	if config == nil {
		config = &types.AuditConfig{
			Enabled:       true,
			LogQueries:    true,
			LogResponses:  false,
			LogErrors:     true,
			RetentionDays: 90,
		}
	}

	return &AuditEngine{
		BaseEngine:  BaseEngine{name: "audit"},
		config:      config,
		records:     make([]types.AuditRecord, 0),
		piiPatterns: []*regexp.Regexp{piiEmail, piiSSN, piiCreditCard, piiPhone},
		maxRecords:  100000,
	}
}

func (e *AuditEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	if e.config.LogQueries {
		e.recordQuery(qc)
	}

	complianceResult := e.checkCompliance(qc)
	if !complianceResult.Continue {
		return complianceResult
	}

	return types.EngineResult{Continue: true}
}

func (e *AuditEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled || !e.config.LogResponses {
		return types.EngineResult{Continue: true}
	}

	if qc.Response != nil && qc.Response.Error != nil && e.config.LogErrors {
		e.recordError(qc)
	}

	return types.EngineResult{Continue: true}
}

func (e *AuditEngine) recordQuery(qc *types.QueryContext) {
	record := types.AuditRecord{
		ID:        generateAuditID(),
		Timestamp: time.Now(),
		User:      qc.User,
		Database:  qc.Database,
		Query:     e.sanitizeQuery(qc.RawQuery),
		Result:    "success",
		Duration:  time.Since(qc.Timestamp),
		ClientIP:  qc.ClientAddr,
	}

	if qc.Response != nil && qc.Response.Error != nil {
		record.Result = "error"
	}

	e.addRecord(record)
}

func (e *AuditEngine) recordError(qc *types.QueryContext) {
	record := types.AuditRecord{
		ID:        generateAuditID(),
		Timestamp: time.Now(),
		User:      qc.User,
		Database:  qc.Database,
		Query:     e.sanitizeQuery(qc.RawQuery),
		Result:    "error",
		Duration:  time.Since(qc.Timestamp),
		ClientIP:  qc.ClientAddr,
	}

	e.addRecord(record)
}

func (e *AuditEngine) addRecord(record types.AuditRecord) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.records = append(e.records, record)

	if len(e.records) > e.maxRecords {
		e.records = e.records[len(e.records)-e.maxRecords:]
	}
}

func (e *AuditEngine) sanitizeQuery(query string) string {
	if query == "" {
		return ""
	}

	for _, pattern := range e.piiPatterns {
		query = pattern.ReplaceAllString(query, "[PII_REDACTED]")
	}

	if len(query) > 1000 {
		query = query[:1000] + "..."
	}

	return query
}

func (e *AuditEngine) checkCompliance(qc *types.QueryContext) types.EngineResult {
	e.mu.RLock()
	rules := e.complianceRules
	e.mu.RUnlock()

	for _, rule := range rules {
		if e.matchRule(qc.RawQuery, rule) {
			action := e.applyComplianceRule(qc, rule)
			if action != "allow" {
				return types.EngineResult{
					Continue: false,
					Error:    fmt.Errorf("compliance violation: %s", rule.Name),
					Metadata: map[string]interface{}{
						"rule":     rule.Name,
						"severity": rule.Severity,
						"action":   action,
					},
				}
			}
		}
	}

	return types.EngineResult{Continue: true}
}

func (e *AuditEngine) matchRule(query string, rule types.ComplianceRule) bool {
	if rule.Pattern == "" {
		return false
	}

	pattern := "(?i)" + rule.Pattern
	matched, err := regexp.MatchString(pattern, query)
	return err == nil && matched
}

func (e *AuditEngine) applyComplianceRule(qc *types.QueryContext, rule types.ComplianceRule) string {
	logger.Warn("Compliance rule triggered: " + rule.Name)

	switch rule.Action {
	case "block":
		return "block"
	case "mask":
		qc.Metadata["compliance_mask"] = true
		return "allow"
	case "alert":
		return "allow"
	default:
		return "allow"
	}
}

func (e *AuditEngine) AddComplianceRule(rule types.ComplianceRule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.complianceRules = append(e.complianceRules, rule)
}

func (e *AuditEngine) RemoveComplianceRule(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	var newRules []types.ComplianceRule
	for _, r := range e.complianceRules {
		if r.Name != name {
			newRules = append(newRules, r)
		}
	}
	e.complianceRules = newRules
}

func (e *AuditEngine) GetRecords() []types.AuditRecord {
	e.mu.RLock()
	defer e.mu.RUnlock()

	records := make([]types.AuditRecord, len(e.records))
	copy(records, e.records)
	return records
}

func (e *AuditEngine) GetRecordsByUser(user string) []types.AuditRecord {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var result []types.AuditRecord
	for _, r := range e.records {
		if r.User == user {
			result = append(result, r)
		}
	}
	return result
}

func (e *AuditEngine) GetRecordsByDatabase(database string) []types.AuditRecord {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var result []types.AuditRecord
	for _, r := range e.records {
		if r.Database == database {
			result = append(result, r)
		}
	}
	return result
}

func (e *AuditEngine) GetRecordsByTimeRange(start, end time.Time) []types.AuditRecord {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var result []types.AuditRecord
	for _, r := range e.records {
		if r.Timestamp.After(start) && r.Timestamp.Before(end) {
			result = append(result, r)
		}
	}
	return result
}

func (e *AuditEngine) ExportToJSON() (string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	data, err := json.MarshalIndent(e.records, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to export audit records: %w", err)
	}

	return string(data), nil
}

func (e *AuditEngine) ClearOldRecords(retentionDays int) {
	e.mu.Lock()
	defer e.mu.Unlock()

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	var newRecords []types.AuditRecord
	for _, r := range e.records {
		if r.Timestamp.After(cutoff) {
			newRecords = append(newRecords, r)
		}
	}
	e.records = newRecords
}

func (e *AuditEngine) SetConfig(config *types.AuditConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config = config
}

func (e *AuditEngine) GetConfig() *types.AuditConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.config
}

func generateAuditID() string {
	return fmt.Sprintf("AUD-%d-%s", time.Now().Unix(), randomString(8))
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
		time.Sleep(time.Nanosecond)
	}
	return string(b)
}

func containsPII(query string) bool {
	lowerQuery := strings.ToLower(query)
	piiKeywords := []string{"password", "credit_card", "ssn", "social_security", "secret", "token"}

	for _, keyword := range piiKeywords {
		if strings.Contains(lowerQuery, keyword) {
			return true
		}
	}

	return false
}
