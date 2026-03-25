package engines

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/udbp/udbproxy/pkg/logger"
	"github.com/udbp/udbproxy/pkg/types"
)

type SecurityEngine struct {
	BaseEngine
	rules []types.SecurityRule
}

var (
	sqlInjectionPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)'(\s*or\s*'?\d*'?\s*=\s*'?\\?\\d)`),
		regexp.MustCompile(`(?i)'(\s*or\s+\w+\s*=\s*\w+)`),
		regexp.MustCompile(`(?i)union\s+select`),
		regexp.MustCompile(`(?i)union\s+all\s+select`),
		regexp.MustCompile(`(?i)insert\s+into`),
		regexp.MustCompile(`(?i)drop\s+table`),
		regexp.MustCompile(`(?i)delete\s+from`),
		regexp.MustCompile(`(?i)update\s+\w+\s+set`),
		regexp.MustCompile(`(?i)alter\s+table`),
		regexp.MustCompile(`--\s*$`),
		regexp.MustCompile(`/\*.*\*/`),
		regexp.MustCompile(`;\s*drop`),
	}
	dangerousFunctions = []string{
		"exec(", "execute(", "eval(", "system(",
		"xp_cmdshell", "sp_executesql",
	}
)

func NewSecurityEngine(rules []types.SecurityRule) *SecurityEngine {
	return &SecurityEngine{
		BaseEngine: BaseEngine{name: "security"},
		rules:      rules,
	}
}

func (e *SecurityEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if qc.RawQuery == "" {
		return types.EngineResult{Continue: true}
	}

	for _, rule := range e.rules {
		if e.matchRule(qc.RawQuery, rule) {
			return e.applyRule(qc, rule)
		}
	}

	if e.detectSQLInjection(qc.RawQuery) {
		logger.Warn("SQL injection attempt detected")

		return types.EngineResult{
			Continue: false,
			Error:    fmt.Errorf("potential SQL injection detected"),
			Metadata: map[string]interface{}{"action": "deny"},
		}
	}

	if e.containsDangerousFunctions(qc.RawQuery) {
		logger.Warn("Dangerous function detected")

		return types.EngineResult{
			Continue: false,
			Error:    fmt.Errorf("dangerous function call not allowed"),
			Metadata: map[string]interface{}{"action": "deny"},
		}
	}

	return types.EngineResult{Continue: true}
}

func (e *SecurityEngine) matchRule(query string, rule types.SecurityRule) bool {
	if rule.MatchPattern == "" {
		return false
	}

	pattern := "(?i)" + rule.MatchPattern
	matched, err := regexp.MatchString(pattern, query)
	return err == nil && matched
}

func (e *SecurityEngine) applyRule(qc *types.QueryContext, rule types.SecurityRule) types.EngineResult {
	logger.Info("Security rule matched")

	switch rule.Action {
	case types.SecurityActionDeny:
		return types.EngineResult{
			Continue: false,
			Error:    fmt.Errorf("query denied by rule: %s", rule.Name),
			Metadata: map[string]interface{}{"rule": rule.Name},
		}
	case types.SecurityActionLog:
		logger.Warn("Query logged by rule")
		return types.EngineResult{Continue: true}
	case types.SecurityActionMask:
		qc.Metadata["masked"] = true
		qc.Metadata["mask_fields"] = rule.MaskFields
		return types.EngineResult{Continue: true}
	default:
		return types.EngineResult{Continue: true}
	}
}

func (e *SecurityEngine) detectSQLInjection(query string) bool {
	for _, pattern := range sqlInjectionPatterns {
		if pattern.MatchString(query) {
			return true
		}
	}
	return false
}

func (e *SecurityEngine) containsDangerousFunctions(query string) bool {
	lowerQuery := strings.ToLower(query)
	for _, fn := range dangerousFunctions {
		if strings.Contains(lowerQuery, fn) {
			return true
		}
	}
	return false
}

func (e *SecurityEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if qc.Response == nil || qc.Response.Data == nil {
		return types.EngineResult{Continue: true}
	}

	masked, ok := qc.Metadata["masked"].(bool)
	if !ok || !masked {
		return types.EngineResult{Continue: true}
	}

	maskFields, ok := qc.Metadata["mask_fields"].([]string)
	if !ok || len(maskFields) == 0 {
		return types.EngineResult{Continue: true}
	}

	for rowIdx := range qc.Response.Data {
		for colIdx, col := range qc.Response.Columns {
			for _, field := range maskFields {
				if col == field && rowIdx < len(qc.Response.Data[rowIdx]) && colIdx < len(qc.Response.Data[rowIdx]) {
					qc.Response.Data[rowIdx][colIdx] = "***MASKED***"
				}
			}
		}
	}

	return types.EngineResult{Continue: true}
}

func (e *SecurityEngine) AddRule(rule types.SecurityRule) {
	e.rules = append(e.rules, rule)
}

func (e *SecurityEngine) RemoveRule(ruleName string) {
	var newRules []types.SecurityRule
	for _, r := range e.rules {
		if r.Name != ruleName {
			newRules = append(newRules, r)
		}
	}
	e.rules = newRules
}
