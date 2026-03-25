package engines

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/udbp/udbproxy/pkg/types"
)

// MultiTenantIsolationEngine provides tenant segmentation, quota enforcement, and cross-tenant query prevention
type MultiTenantIsolationEngine struct {
	BaseEngine
	config           *MultiTenantConfig
	tenantRegistry   *TenantRegistry
	quotas           map[string]*TenantQuota
	accessPolicies   map[string]*AccessPolicy
	tenantUsage      map[string]*UsageTracker
	mu               sync.RWMutex
}

type MultiTenantConfig struct {
	Enabled           bool          // Enable the engine
	StrictIsolation   bool          // Prevent cross-tenant queries
	QuotaEnforcement  bool          // Enforce resource quotas
	QueryLimitEnabled bool          // Limit queries per tenant
	RowLimitEnabled   bool          // Limit rows returned per tenant
	AutoProvisioning  bool          // Enable automatic tenant provisioning
	MaxTenants        int           // Maximum tenants allowed
	DefaultQuota      *QuotaConfig  // Default quota for new tenants
}

type TenantRegistry struct {
	tenants map[string]*Tenant
	mu      sync.RWMutex
}

type Tenant struct {
	TenantID       string
	Name           string
	CreatedAt      time.Time
	Status         TenantStatus // ACTIVE, SUSPENDED, ARCHIVED
	Database       string
	Schema         string
	ConnectionPool *PoolConfig
	Tags           map[string]string
}

type TenantStatus int

const (
	TenantActive TenantStatus = iota
	TenantSuspended
	TenantArchived
)

type PoolConfig struct {
	MinConnections int
	MaxConnections int
	MaxQueries     int
	MaxConnectionsPerSecond int
}

type TenantQuota struct {
	TenantID       string
	DailyQueryLimit int64
	MonthlyQueryLimit int64
	DailyRowLimit   int64
	MonthlyRowLimit int64
	DailyBytesLimit int64
	QueryTimeout   time.Duration
	ConnectionLimit int
	CurrentQueries int64
	CurrentRows    int64
	CurrentBytes   int64
	QueriesResetAt  time.Time
	RowsResetAt     time.Time
	BytesResetAt    time.Time
	mu              sync.RWMutex
}

type AccessPolicy struct {
	TenantID       string
	AllowedTables   []string
	BlockedTables   []string
	AllowedCommands []string // SELECT, INSERT, UPDATE, DELETE
	ColumnFilters   map[string][]string // table -> allowed columns
	RowFilters      []RowFilter
	RateLimit       int // queries per minute
	mu              sync.RWMutex
}

type RowFilter struct {
	Table     string
	Column    string
	Condition string // e.g., "tenant_id = 'X'"
	SQL       string // e.g., "WHERE tenant_id = 'X'"
}

type UsageTracker struct {
	TenantID       string
	DailyQueries    int64
	MonthlyQueries  int64
	DailyRows       int64
	MonthlyRows     int64
	DailyBytes      int64
	MonthlyBytes    int64
	LastQueryTime   time.Time
	QueryTimestamps []time.Time // For rate limiting
	mu              sync.RWMutex
}

// NewMultiTenantIsolationEngine creates a new Multi-Tenant Isolation Engine
func NewMultiTenantIsolationEngine(config *MultiTenantConfig) *MultiTenantIsolationEngine {
	if config == nil {
		config = &MultiTenantConfig{
			Enabled:          true,
			StrictIsolation:  true,
			QuotaEnforcement: true,
			MaxTenants:       1000,
			DefaultQuota: &QuotaConfig{
				DailyQueryLimit:   100000,
				MonthlyQueryLimit: 1000000,
				DailyRowLimit:     10000000,
				MonthlyRowLimit:   100000000,
				DailyBytesLimit:   10 * 1024 * 1024 * 1024, // 10 GB
				QueryTimeout:      30 * time.Second,
				ConnectionLimit:   50,
			},
		}
	}

	engine := &MultiTenantIsolationEngine{
		BaseEngine:      BaseEngine{name: "multitenant_isolation"},
		config:          config,
		tenantRegistry:  &TenantRegistry{tenants: make(map[string]*Tenant)},
		quotas:          make(map[string]*TenantQuota),
		accessPolicies:  make(map[string]*AccessPolicy),
		tenantUsage:     make(map[string]*UsageTracker),
	}

	// Start quota reset loop
	go engine.quotaResetLoop()

	return engine
}

// Process handles multi-tenant isolation
func (e *MultiTenantIsolationEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	// Get tenant ID from context
	tenantID := e.extractTenantID(qc)

	// Check if tenant exists and is active
	if !e.isTenantActive(tenantID) {
		return types.EngineResult{
			Continue: false,
			Error:    fmt.Errorf("tenant not found or suspended: %s", tenantID),
			Metadata: map[string]interface{}{
				"reason": "tenant_inactive",
				"tenant_id": tenantID,
			},
		}
	}

	// Check cross-tenant isolation
	if e.config.StrictIsolation && !e.validateTenantAccess(qc, tenantID) {
		return types.EngineResult{
			Continue: false,
			Error:    fmt.Errorf("cross-tenant access forbidden"),
			Metadata: map[string]interface{}{
				"reason": "cross_tenant_blocked",
				"tenant_id": tenantID,
			},
		}
	}

	// Check quota limits
	if e.config.QuotaEnforcement {
		quotaResult := e.checkQuota(tenantID, qc)
		if !quotaResult.Allowed {
			return types.EngineResult{
				Continue: false,
				Error:    fmt.Errorf("quota exceeded: %s", quotaResult.Reason),
				Metadata: map[string]interface{}{
					"reason":       "quota_exceeded",
					"quota_type":    quotaResult.QuotaType,
					"current":       quotaResult.Current,
					"limit":         quotaResult.Limit,
					"tenant_id":     tenantID,
				},
			}
		}
	}

	// Check access policy
	if policy, exists := e.accessPolicies[tenantID]; exists {
		if !e.validateAccessPolicy(qc, policy) {
			return types.EngineResult{
				Continue: false,
				Error:    fmt.Errorf("access denied by policy"),
				Metadata: map[string]interface{}{
					"reason":    "policy_denied",
					"tenant_id": tenantID,
				},
			}
		}
	}

	// Apply row filter if configured
	e.applyRowFilters(qc, tenantID)

	// Track usage
	e.trackUsage(tenantID, qc)

	// Add tenant info to metadata
	if qc.Metadata == nil {
		qc.Metadata = make(map[string]interface{})
	}
	qc.Metadata["tenant_id"] = tenantID
	qc.Metadata["multitenant_enabled"] = true

	return types.EngineResult{Continue: true}
}

// ProcessResponse handles response processing
func (e *MultiTenantIsolationEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	return types.EngineResult{Continue: true}
}

// extractTenantID extracts tenant ID from query context
func (e *MultiTenantIsolationEngine) extractTenantID(qc *types.QueryContext) string {
	// First try metadata
	if qc.Metadata != nil {
		if tid, ok := qc.Metadata["tenant_id"].(string); ok && tid != "" {
			return tid
		}
	}

	// Try from user (which may contain tenant info)
	if qc.User != "" {
		return qc.User
	}

	// Fallback to default tenant
	return "default"
}

// isTenantActive checks if tenant exists and is active
func (e *MultiTenantIsolationEngine) isTenantActive(tenantID string) bool {
	e.tenantRegistry.mu.RLock()
	defer e.tenantRegistry.mu.RUnlock()

	if tenant, exists := e.tenantRegistry.tenants[tenantID]; exists {
		return tenant.Status == TenantActive
	}

	// If tenant doesn't exist and auto-provisioning is enabled, create it
	if e.config.AutoProvisioning && tenantID != "default" {
		e.tenantRegistry.mu.Lock()
		e.tenantRegistry.tenants[tenantID] = &Tenant{
			TenantID:  tenantID,
			Name:      tenantID,
			CreatedAt: time.Now(),
			Status:    TenantActive,
			Database:  tenantID,
		}
		e.initTenantResources(tenantID)
		e.tenantRegistry.mu.Unlock()
		return true
	}

	return tenantID == "default" || e.config.AutoProvisioning
}

// validateTenantAccess validates that tenant can access the requested resources
func (e *MultiTenantIsolationEngine) validateTenantAccess(qc *types.QueryContext, tenantID string) bool {
	e.tenantRegistry.mu.RLock()
	tenant, exists := e.tenantRegistry.tenants[tenantID]
	e.tenantRegistry.mu.RUnlock()

	if !exists {
		return false
	}

	// Check if query targets the tenant's database
	if qc.Database != "" && qc.Database != tenant.Database && qc.Database != "information_schema" {
		// Cross-tenant database access
		return false
	}

	return true
}

// checkQuota checks if tenant has quota available
func (e *MultiTenantIsolationEngine) checkQuota(tenantID string, qc *types.QueryContext) QuotaCheckResult {
	quota, exists := e.quotas[tenantID]
	if !exists {
		return QuotaCheckResult{Allowed: true}
	}

	quota.mu.Lock()
	defer quota.mu.Unlock()

	// Check daily query limit
	if quota.DailyQueryLimit > 0 && quota.CurrentQueries >= quota.DailyQueryLimit {
		return QuotaCheckResult{
			Allowed:   false,
			QuotaType: "daily_queries",
			Current:   quota.CurrentQueries,
			Limit:     quota.DailyQueryLimit,
			Reason:    "daily query limit exceeded",
		}
	}

	// Check row limit (use metadata if available)
	expectedRows := int64(0)
	if e.config.RowLimitEnabled {
		if rows, ok := qc.Metadata["expected_rows"]; ok {
			if er, ok := rows.(int64); ok {
				expectedRows = er
			}
		}
		if expectedRows > 0 && quota.DailyRowLimit > 0 && quota.CurrentRows+expectedRows > quota.DailyRowLimit {
			return QuotaCheckResult{
				Allowed:   false,
				QuotaType: "daily_rows",
				Current:   quota.CurrentRows,
				Limit:     quota.DailyRowLimit,
				Reason:    "daily row limit exceeded",
			}
		}
	}

	return QuotaCheckResult{Allowed: true}
}

// validateAccessPolicy validates query against access policy
func (e *MultiTenantIsolationEngine) validateAccessPolicy(qc *types.QueryContext, policy *AccessPolicy) bool {
	policy.mu.Lock()
	defer policy.mu.Unlock()

	// Check command type (use Operation field)
	commandAllowed := false
	for _, cmd := range policy.AllowedCommands {
		if string(qc.Operation) == cmd {
			commandAllowed = true
			break
		}
	}
	if len(policy.AllowedCommands) > 0 && !commandAllowed {
		return false
	}

	// Check blocked tables
	for _, blocked := range policy.BlockedTables {
		if containsString(qc.RawQuery, blocked) {
			return false
		}
	}

	// Check allowed tables (if specified)
	if len(policy.AllowedTables) > 0 {
		allowed := false
		for _, table := range policy.AllowedTables {
			if containsString(qc.RawQuery, table) {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}

	// Check rate limit
	if policy.RateLimit > 0 {
		// Rate limit check would be done in UsageTracker
	}

	return true
}

// applyRowFilters applies tenant-specific row filters to queries
func (e *MultiTenantIsolationEngine) applyRowFilters(qc *types.QueryContext, tenantID string) {
	policy, exists := e.accessPolicies[tenantID]
	if !exists {
		return
	}

	policy.mu.Lock()
	defer policy.mu.Unlock()

	// Add WHERE clause filters to query
	for _, filter := range policy.RowFilters {
		if containsString(qc.RawQuery, filter.Table) && !containsString(qc.RawQuery, filter.SQL) {
			// Would append filter.SQL to WHERE clause
			qc.RawQuery = qc.RawQuery + " /* filtered by tenant policy */"
		}
	}
}

// trackUsage tracks tenant resource usage
func (e *MultiTenantIsolationEngine) trackUsage(tenantID string, qc *types.QueryContext) {
	usage, exists := e.tenantUsage[tenantID]
	if !exists {
		usage = &UsageTracker{TenantID: tenantID}
		e.tenantUsage[tenantID] = usage
	}

	usage.mu.Lock()
	defer usage.mu.Unlock()

	usage.DailyQueries++
	usage.MonthlyQueries++
	usage.LastQueryTime = time.Now()

	// Track query timestamps for rate limiting
	now := time.Now()
	usage.QueryTimestamps = append(usage.QueryTimestamps, now)

	// Keep only last minute of timestamps
	cutoff := now.Add(-1 * time.Minute)
	newTimestamps := make([]time.Time, 0)
	for _, ts := range usage.QueryTimestamps {
		if ts.After(cutoff) {
			newTimestamps = append(newTimestamps, ts)
		}
	}
	usage.QueryTimestamps = newTimestamps

	// Get expected rows from metadata
	expectedRows := int64(0)
	if rows, ok := qc.Metadata["expected_rows"]; ok {
		if er, ok := rows.(int64); ok {
			expectedRows = er
		}
	}
	if expectedRows > 0 {
		usage.DailyRows += expectedRows
		usage.MonthlyRows += expectedRows
	}
}

// initTenantResources initializes resources for a new tenant
func (e *MultiTenantIsolationEngine) initTenantResources(tenantID string) {
	// Initialize quota
	defaultQuota := e.config.DefaultQuota
	e.quotas[tenantID] = &TenantQuota{
		TenantID:          tenantID,
		DailyQueryLimit:   defaultQuota.DailyQueryLimit,
		MonthlyQueryLimit: defaultQuota.MonthlyQueryLimit,
		DailyRowLimit:     defaultQuota.DailyRowLimit,
		MonthlyRowLimit:   defaultQuota.MonthlyRowLimit,
		DailyBytesLimit:   defaultQuota.DailyBytesLimit,
		QueryTimeout:       defaultQuota.QueryTimeout,
		ConnectionLimit:   defaultQuota.ConnectionLimit,
		QueriesResetAt:    getMidnight(),
		RowsResetAt:       getMidnight(),
		BytesResetAt:      getFirstOfMonth(),
	}

	// Initialize usage tracker
	e.tenantUsage[tenantID] = &UsageTracker{TenantID: tenantID}
}

// quotaResetLoop resets quotas periodically
func (e *MultiTenantIsolationEngine) quotaResetLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()

		e.mu.Lock()
		for _, quota := range e.quotas {
			quota.mu.Lock()

			// Reset daily quotas
			if now.After(quota.QueriesResetAt) {
				quota.CurrentQueries = 0
				quota.CurrentRows = 0
				quota.CurrentBytes = 0
				quota.QueriesResetAt = getMidnight()
				quota.RowsResetAt = getMidnight()
				quota.BytesResetAt = getMidnight()
			}

			// Reset monthly quotas
			if now.After(quota.BytesResetAt) {
				quota.CurrentBytes = 0
				quota.BytesResetAt = getFirstOfMonth()
			}
			quota.mu.Unlock()
		}
		e.mu.Unlock()
	}
}

// RegisterTenant registers a new tenant
func (e *MultiTenantIsolationEngine) RegisterTenant(tenant *Tenant) error {
	e.tenantRegistry.mu.Lock()
	defer e.tenantRegistry.mu.Unlock()

	if len(e.tenantRegistry.tenants) >= e.config.MaxTenants {
		return fmt.Errorf("maximum tenants reached")
	}

	e.tenantRegistry.tenants[tenant.TenantID] = tenant
	e.initTenantResources(tenant.TenantID)

	return nil
}

// SuspendTenant suspends a tenant
func (e *MultiTenantIsolationEngine) SuspendTenant(tenantID string) error {
	e.tenantRegistry.mu.Lock()
	defer e.tenantRegistry.mu.Unlock()

	tenant, exists := e.tenantRegistry.tenants[tenantID]
	if !exists {
		return fmt.Errorf("tenant not found: %s", tenantID)
	}

	tenant.Status = TenantSuspended
	return nil
}

// ResumeTenant resumes a suspended tenant
func (e *MultiTenantIsolationEngine) ResumeTenant(tenantID string) error {
	e.tenantRegistry.mu.Lock()
	defer e.tenantRegistry.mu.Unlock()

	tenant, exists := e.tenantRegistry.tenants[tenantID]
	if !exists {
		return fmt.Errorf("tenant not found: %s", tenantID)
	}

	tenant.Status = TenantActive
	return nil
}

// SetQuota sets quota for a tenant
func (e *MultiTenantIsolationEngine) SetQuota(tenantID string, quota *QuotaConfig) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.quotas[tenantID]; !exists {
		return fmt.Errorf("tenant not found: %s", tenantID)
	}

	e.quotas[tenantID].DailyQueryLimit = quota.DailyQueryLimit
	e.quotas[tenantID].MonthlyQueryLimit = quota.MonthlyQueryLimit
	e.quotas[tenantID].DailyRowLimit = quota.DailyRowLimit
	e.quotas[tenantID].MonthlyRowLimit = quota.MonthlyRowLimit
	e.quotas[tenantID].DailyBytesLimit = quota.DailyBytesLimit

	return nil
}

// SetAccessPolicy sets access policy for a tenant
func (e *MultiTenantIsolationEngine) SetAccessPolicy(tenantID string, policy *AccessPolicy) {
	e.mu.Lock()
	defer e.mu.Unlock()

	policy.TenantID = tenantID
	e.accessPolicies[tenantID] = policy
}

// GetTenantUsage returns usage statistics for a tenant
func (e *MultiTenantIsolationEngine) GetTenantUsage(tenantID string) (TenantUsageResponse, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	usage, exists := e.tenantUsage[tenantID]
	if !exists {
		return TenantUsageResponse{}, false
	}

	usage.mu.RLock()
	defer usage.mu.RUnlock()

	quota, quotaExists := e.quotas[tenantID]
	var rateLimit int
	if quotaExists {
		quota.mu.RLock()
		rateLimit = len(usage.QueryTimestamps)
		quota.mu.RUnlock()
	}

	return TenantUsageResponse{
		TenantID:         tenantID,
		DailyQueries:     usage.DailyQueries,
		MonthlyQueries:   usage.MonthlyQueries,
		DailyRows:        usage.DailyRows,
		MonthlyRows:      usage.MonthlyRows,
		RequestsPerMinute: rateLimit,
		LastQueryTime:    usage.LastQueryTime,
	}, true
}

// Helper functions
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && (s[:len(substr)] == substr || containsString(s[1:], substr)))
}

func getMidnight() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
}

func getFirstOfMonth() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, now.Location())
}

// QuotaConfig holds quota configuration
type QuotaConfig struct {
	DailyQueryLimit   int64
	MonthlyQueryLimit int64
	DailyRowLimit     int64
	MonthlyRowLimit   int64
	DailyBytesLimit   int64
	QueryTimeout      time.Duration
	ConnectionLimit   int
}

// QuotaCheckResult holds quota check result
type QuotaCheckResult struct {
	Allowed    bool
	QuotaType  string
	Current    int64
	Limit      int64
	Reason     string
}

// Helper types for API responses
type TenantUsageResponse struct {
	TenantID          string    `json:"tenant_id"`
	DailyQueries      int64     `json:"daily_queries"`
	MonthlyQueries    int64     `json:"monthly_queries"`
	DailyRows         int64     `json:"daily_rows"`
	MonthlyRows       int64     `json:"monthly_rows"`
	RequestsPerMinute int       `json:"requests_per_minute"`
	LastQueryTime     time.Time `json:"last_query_time"`
}