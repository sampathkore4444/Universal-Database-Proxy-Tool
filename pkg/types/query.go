package types

import (
	"context"
	"time"
)

type DatabaseType string

const (
	DatabaseTypeMySQL    DatabaseType = "mysql"
	DatabaseTypePostgres DatabaseType = "postgres"
	DatabaseTypeMongoDB  DatabaseType = "mongodb"
	DatabaseTypeRedis    DatabaseType = "redis"
	DatabaseTypeMSSQL    DatabaseType = "mssql"
	DatabaseTypeOracle   DatabaseType = "oracle"
)

type QueryContext struct {
	ID              string
	RawQuery        string
	NormalizedQuery string
	DatabaseType    DatabaseType
	Database        string
	Table           string
	Operation       QueryOperation
	User            string
	ClientAddr      string
	Timestamp       time.Time
	Metadata        map[string]interface{}
	Response        *QueryResponse
	SpanContext     interface{}
}

type QueryOperation string

const (
	OpSelect QueryOperation = "SELECT"
	OpInsert QueryOperation = "INSERT"
	OpUpdate QueryOperation = "UPDATE"
	OpDelete QueryOperation = "DELETE"
	OpOther  QueryOperation = "OTHER"
)

type QueryResponse struct {
	RowsAffected int64
	RowsReturned int64
	Columns      []string
	Data         [][]interface{}
	Error        error
	Duration     time.Duration
}

type EngineResult struct {
	Continue bool
	Modified bool
	Error    error
	Metadata map[string]interface{}
}

type Engine interface {
	Name() string
	Process(ctx context.Context, qc *QueryContext) EngineResult
	ProcessResponse(ctx context.Context, qc *QueryContext) EngineResult
}

type EngineRegistry map[string]Engine

type ConnectionPool struct {
	MaxConnections int
	MaxIdleTime    time.Duration
	MaxLifetime    time.Duration
}

type DatabaseConfig struct {
	Name          string
	Type          DatabaseType
	Host          string
	Port          int
	Database      string
	Username      string
	Password      string
	SSLMode       bool
	Pool          *ConnectionPool
	Tags          map[string]string
	IsReadReplica bool
}

type RoutingRule struct {
	Name          string
	MatchPattern  string
	Database      string
	IsReadReplica bool
	Priority      int
}

type SecurityRule struct {
	Name         string
	MatchPattern string
	Action       SecurityAction
	MaskFields   []string
	LogOnly      bool
}

type SecurityAction string

const (
	SecurityActionAllow SecurityAction = "ALLOW"
	SecurityActionDeny  SecurityAction = "DENY"
	SecurityActionMask  SecurityAction = "MASK"
	SecurityActionLog   SecurityAction = "LOG"
)

type CacheConfig struct {
	Enabled   bool
	Backend   string
	TTL       time.Duration
	MaxSize   int64
	KeyPrefix string
}

type ObservabilityConfig struct {
	Enabled            bool
	LogQueries         bool
	LogResponses       bool
	SlowQueryThreshold time.Duration
	MetricsEnabled     bool
	TracingEnabled     bool
}

type ProxyConfig struct {
	ListenAddress string
	ListenPort    int
	TLSEnabled    bool
	TLSCertFile   string
	TLSKeyFile    string
	Databases     []DatabaseConfig
	RoutingRules  []RoutingRule
	SecurityRules []SecurityRule
	Cache         *CacheConfig
	Observability *ObservabilityConfig
	Engines       []string
}

type TransformRule struct {
	Name         string
	MatchPattern string
	ReplaceWith  string
	Action       TransformAction
	MaskFields   []string
}

type TransformAction string

const (
	TransformActionRewrite  TransformAction = "rewrite"
	TransformActionAbstract TransformAction = "abstract"
	TransformActionMask     TransformAction = "mask"
	TransformActionRemove   TransformAction = "remove"
)

type RateLimitConfig struct {
	Enabled           bool
	RequestsPerSecond int
	BurstSize         int
	ByUser            bool
	ByDatabase        bool
	ByIP              bool
}

type AuditConfig struct {
	Enabled       bool
	LogQueries    bool
	LogResponses  bool
	LogErrors     bool
	RetentionDays int
	StorageType   string
}

type ComplianceRule struct {
	Name     string
	Type     string
	Pattern  string
	Action   string
	Severity string
}

type AuditRecord struct {
	ID        string
	Timestamp time.Time
	User      string
	Database  string
	Query     string
	Result    string
	Duration  time.Duration
	ClientIP  string
}
