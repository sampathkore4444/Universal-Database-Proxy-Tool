package config

import (
	"fmt"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
	"github.com/udbp/udbproxy/pkg/types"
)

type Config struct {
	Server        ServerConfig         `mapstructure:"server"`
	Databases     []DatabaseConfig     `mapstructure:"databases"`
	RoutingRules  []RoutingRuleConfig  `mapstructure:"routing_rules"`
	SecurityRules []SecurityRuleConfig `mapstructure:"security_rules"`
	Cache         CacheConfig          `mapstructure:"cache"`
	Observability ObservabilityConfig  `mapstructure:"observability"`
	Logging       LoggingConfig        `mapstructure:"logging"`
	Metrics       MetricsConfig        `mapstructure:"metrics"`
	Plugins       PluginConfig         `mapstructure:"plugins"`
	TLS           TLSConfig            `mapstructure:"tls"`
}

type ServerConfig struct {
	ListenAddress string `mapstructure:"listen_address"`
	ListenPort    int    `mapstructure:"listen_port"`
	ReadTimeout   int    `mapstructure:"read_timeout"`
	WriteTimeout  int    `mapstructure:"write_timeout"`
	IdleTimeout   int    `mapstructure:"idle_timeout"`
	MaxClients    int    `mapstructure:"max_clients"`
}

type DatabaseConfig struct {
	Name           string            `mapstructure:"name"`
	Type           string            `mapstructure:"type"`
	Host           string            `mapstructure:"host"`
	Port           int               `mapstructure:"port"`
	Database       string            `mapstructure:"database"`
	Username       string            `mapstructure:"username"`
	Password       string            `mapstructure:"password"`
	SSLMode        bool              `mapstructure:"ssl_mode"`
	MaxConnections int               `mapstructure:"max_connections"`
	MaxIdleTime    int               `mapstructure:"max_idle_time"`
	MaxLifetime    int               `mapstructure:"max_lifetime"`
	IsReadReplica  bool              `mapstructure:"is_read_replica"`
	Tags           map[string]string `mapstructure:"tags"`
}

type RoutingRuleConfig struct {
	Name          string `mapstructure:"name"`
	MatchPattern  string `mapstructure:"match_pattern"`
	Database      string `mapstructure:"database"`
	IsReadReplica bool   `mapstructure:"is_read_replica"`
	Priority      int    `mapstructure:"priority"`
}

type SecurityRuleConfig struct {
	Name         string   `mapstructure:"name"`
	MatchPattern string   `mapstructure:"match_pattern"`
	Action       string   `mapstructure:"action"`
	MaskFields   []string `mapstructure:"mask_fields"`
	LogOnly      bool     `mapstructure:"log_only"`
}

type CacheConfig struct {
	Enabled   bool   `mapstructure:"enabled"`
	Backend   string `mapstructure:"backend"`
	TTL       int    `mapstructure:"ttl"`
	MaxSize   int64  `mapstructure:"max_size"`
	KeyPrefix string `mapstructure:"key_prefix"`
	RedisURL  string `mapstructure:"redis_url"`
}

type ObservabilityConfig struct {
	Enabled            bool `mapstructure:"enabled"`
	LogQueries         bool `mapstructure:"log_queries"`
	LogResponses       bool `mapstructure:"log_responses"`
	SlowQueryThreshold int  `mapstructure:"slow_query_threshold"`
	MetricsEnabled     bool `mapstructure:"metrics_enabled"`
	TracingEnabled     bool `mapstructure:"tracing_enabled"`
	MetricsPort        int  `mapstructure:"metrics_port"`
}

type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
	Output string `mapstructure:"output"`
}

type MetricsConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Port    int    `mapstructure:"port"`
	Path    string `mapstructure:"path"`
}

type PluginConfig struct {
	Enabled     bool     `mapstructure:"enabled"`
	Directories []string `mapstructure:"directories"`
	AllowList   []string `mapstructure:"allow_list"`
}

type TLSConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	CertFile   string `mapstructure:"cert_file"`
	KeyFile    string `mapstructure:"key_file"`
	ClientAuth string `mapstructure:"client_auth"`
}

func Load(configPath string) (*Config, error) {
	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	viper.SetDefault("server.listen_address", "0.0.0.0")
	viper.SetDefault("server.listen_port", 5432)
	viper.SetDefault("server.read_timeout", 30)
	viper.SetDefault("server.write_timeout", 30)
	viper.SetDefault("server.idle_timeout", 120)
	viper.SetDefault("server.max_clients", 1000)

	viper.SetDefault("cache.enabled", false)
	viper.SetDefault("cache.backend", "memory")
	viper.SetDefault("cache.ttl", 300)
	viper.SetDefault("cache.max_size", 10000)

	viper.SetDefault("observability.enabled", true)
	viper.SetDefault("observability.log_queries", true)
	viper.SetDefault("observability.slow_query_threshold", 1000)
	viper.SetDefault("observability.metrics_enabled", true)
	viper.SetDefault("observability.metrics_port", 9090)

	viper.SetDefault("logging.level", "info")
	viper.SetDefault("logging.format", "json")

	viper.SetDefault("metrics.enabled", true)
	viper.SetDefault("metrics.port", 8080)
	viper.SetDefault("metrics.path", "/metrics")

	viper.SetDefault("tls.enabled", false)

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &config, nil
}

func LoadDefault() *Config {
	return &Config{
		Server: ServerConfig{
			ListenAddress: "0.0.0.0",
			ListenPort:    5432,
			ReadTimeout:   30,
			WriteTimeout:  30,
			IdleTimeout:   120,
			MaxClients:    1000,
		},
		Cache: CacheConfig{
			Enabled:   false,
			Backend:   "memory",
			TTL:       300,
			MaxSize:   10000,
			KeyPrefix: "udbproxy",
		},
		Observability: ObservabilityConfig{
			Enabled:            true,
			LogQueries:         true,
			LogResponses:       false,
			SlowQueryThreshold: 1000,
			MetricsEnabled:     true,
			MetricsPort:        9090,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
		},
		Metrics: MetricsConfig{
			Enabled: true,
			Port:    8080,
			Path:    "/metrics",
		},
		TLS: TLSConfig{
			Enabled: false,
		},
	}
}

func (c *Config) ToProxyConfig() *types.ProxyConfig {
	proxyConfig := &types.ProxyConfig{
		ListenAddress: c.Server.ListenAddress,
		ListenPort:    c.Server.ListenPort,
		TLSEnabled:    c.TLS.Enabled,
		TLSCertFile:   c.TLS.CertFile,
		TLSKeyFile:    c.TLS.KeyFile,
		Cache: &types.CacheConfig{
			Enabled:   c.Cache.Enabled,
			Backend:   c.Cache.Backend,
			TTL:       time.Duration(c.Cache.TTL) * time.Second,
			MaxSize:   c.Cache.MaxSize,
			KeyPrefix: c.Cache.KeyPrefix,
		},
		Observability: &types.ObservabilityConfig{
			Enabled:            c.Observability.Enabled,
			LogQueries:         c.Observability.LogQueries,
			LogResponses:       c.Observability.LogResponses,
			SlowQueryThreshold: time.Duration(c.Observability.SlowQueryThreshold) * time.Millisecond,
			MetricsEnabled:     c.Observability.MetricsEnabled,
			TracingEnabled:     c.Observability.TracingEnabled,
		},
	}

	for _, db := range c.Databases {
		pool := &types.ConnectionPool{
			MaxConnections: db.MaxConnections,
			MaxIdleTime:    time.Duration(db.MaxIdleTime) * time.Second,
			MaxLifetime:    time.Duration(db.MaxLifetime) * time.Second,
		}

		proxyConfig.Databases = append(proxyConfig.Databases, types.DatabaseConfig{
			Name:          db.Name,
			Type:          types.DatabaseType(db.Type),
			Host:          db.Host,
			Port:          db.Port,
			Database:      db.Database,
			Username:      db.Username,
			Password:      db.Password,
			SSLMode:       db.SSLMode,
			Pool:          pool,
			Tags:          db.Tags,
			IsReadReplica: db.IsReadReplica,
		})
	}

	for _, r := range c.RoutingRules {
		proxyConfig.RoutingRules = append(proxyConfig.RoutingRules, types.RoutingRule{
			Name:          r.Name,
			MatchPattern:  r.MatchPattern,
			Database:      r.Database,
			IsReadReplica: r.IsReadReplica,
			Priority:      r.Priority,
		})
	}

	for _, r := range c.SecurityRules {
		action := types.SecurityAction(r.Action)
		proxyConfig.SecurityRules = append(proxyConfig.SecurityRules, types.SecurityRule{
			Name:         r.Name,
			MatchPattern: r.MatchPattern,
			Action:       action,
			MaskFields:   r.MaskFields,
			LogOnly:      r.LogOnly,
		})
	}

	return proxyConfig
}

func (c *Config) Validate() error {
	if c.Server.ListenPort < 1 || c.Server.ListenPort > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.ListenPort)
	}

	if len(c.Databases) == 0 {
		return fmt.Errorf("at least one database must be configured")
	}

	dbNames := make(map[string]bool)
	for _, db := range c.Databases {
		if db.Name == "" {
			return fmt.Errorf("database name is required")
		}
		if dbNames[db.Name] {
			return fmt.Errorf("duplicate database name: %s", db.Name)
		}
		dbNames[db.Name] = true
	}

	if c.Cache.Enabled && c.Cache.Backend == "redis" && c.Cache.RedisURL == "" {
		return fmt.Errorf("redis_url is required when cache backend is redis")
	}

	return nil
}

func (c *Config) WatchChanges(callback func()) {
	viper.WatchConfig()
	viper.OnConfigChange(func(in fsnotify.Event) {
		callback()
	})
}
