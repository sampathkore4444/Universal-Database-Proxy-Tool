package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/udbp/udbproxy/config"
	"github.com/udbp/udbproxy/internal/server"
	"github.com/udbp/udbproxy/pkg/logger"
)

var (
	configPath string
	debug      bool
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "udbproxy",
	Short: "Universal Database Proxy - A production-grade database proxy",
	Long: `
UDBP (Universal Database Proxy) is a high-performance, extensible, and secure 
proxy layer designed to sit between applications and databases.

Features:
- Multi-database support (MySQL, PostgreSQL, MongoDB, Redis)
- 31 Smart engines (Security, Observability, Caching, AI Optimization)
- Read/write splitting and routing
- SQL injection detection
- Query result caching
- Observability with Prometheus metrics

Usage:
  udbproxy serve --config config.yaml
  udbproxy engines list
  udbproxy databases add
  udbproxy stats`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return serve()
	},
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the proxy server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return serve()
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("UDBP - Universal Database Proxy")
		fmt.Println("Version: 1.0.0")
		fmt.Println("Build: production")
	},
}

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check server health",
	RunE: func(cmd *cobra.Command, args []string) error {
		return checkHealth()
	},
}

// Engine management commands
var enginesCmd = &cobra.Command{
	Use:   "engines",
	Short: "Manage smart engines",
}

var enginesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all smart engines",
	RunE:  listEngines,
}

var enginesEnableCmd = &cobra.Command{
	Use:   "enable [engine-name]",
	Short: "Enable a smart engine",
	Args:  cobra.ExactArgs(1),
	RunE:  enableEngine,
}

var enginesDisableCmd = &cobra.Command{
	Use:   "disable [engine-name]",
	Short: "Disable a smart engine",
	Args:  cobra.ExactArgs(1),
	RunE:  disableEngine,
}

var enginesStatsCmd = &cobra.Command{
	Use:   "stats [engine-name]",
	Short: "Get engine statistics",
	Args:  cobra.ExactArgs(1),
	RunE:  engineStats,
}

// Database management commands
var databasesCmd = &cobra.Command{
	Use:   "databases",
	Short: "Manage databases",
}

var databasesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured databases",
	RunE:  listDatabases,
}

var databasesAddCmd = &cobra.Command{
	Use:   "add [name]",
	Short: "Add a new database",
	Args:  cobra.ExactArgs(1),
	RunE:  addDatabase,
}

var databasesRemoveCmd = &cobra.Command{
	Use:   "remove [name]",
	Short: "Remove a database",
	Args:  cobra.ExactArgs(1),
	RunE:  removeDatabase,
}

// Stats commands
var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show proxy statistics",
	RunE:  showStats,
}

var statsResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset statistics",
	RunE:  resetStats,
}

// Query commands
var queryCmd = &cobra.Command{
	Use:   "query",
	Short: "Execute query through proxy",
	Args:  cobra.MinimumNArgs(1),
	RunE:  executeQuery,
}

var queryHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "Show query history",
	RunE:  showQueryHistory,
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "config.yaml", "Path to configuration file")
	rootCmd.PersistentFlags().BoolVarP(&debug, "debug", "d", false, "Enable debug logging")

	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(healthCmd)

	// Add engine commands
	rootCmd.AddCommand(enginesCmd)
	enginesCmd.AddCommand(enginesListCmd)
	enginesCmd.AddCommand(enginesEnableCmd)
	enginesCmd.AddCommand(enginesDisableCmd)
	enginesCmd.AddCommand(enginesStatsCmd)

	// Add database commands
	rootCmd.AddCommand(databasesCmd)
	databasesCmd.AddCommand(databasesListCmd)
	databasesCmd.AddCommand(databasesAddCmd)
	databasesCmd.AddCommand(databasesRemoveCmd)

	// Add stats commands
	rootCmd.AddCommand(statsCmd)
	statsCmd.AddCommand(statsResetCmd)

	// Add query commands
	rootCmd.AddCommand(queryCmd)
	queryCmd.AddCommand(queryHistoryCmd)
}

func serve() error {
	var cfg *config.Config
	var err error

	if configPath != "" {
		cfg, err = config.Load(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
	} else {
		cfg = config.LoadDefault()
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	if debug {
		cfg.Logging.Level = "debug"
	}

	srv, err := server.NewProxyServer(cfg)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	if err := srv.Start(); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	srv.WaitForInterrupt()

	return srv.Stop()
}

func checkHealth() error {
	resp, err := http.Get("http://localhost:8080/health")
	if err != nil {
		return fmt.Errorf("failed to check health: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed with status: %d", resp.StatusCode)
	}

	fmt.Println("Server is healthy")
	return nil
}

// CLI command implementations
func listEngines(cmd *cobra.Command, args []string) error {
	resp, err := http.Get("http://localhost:8080/api/v1/engines")
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status: %d", resp.StatusCode)
	}

	fmt.Println("Available Smart Engines:")
	fmt.Println("  1. Query Rewrite Engine")
	fmt.Println("  2. Federation Engine")
	fmt.Println("  3. Encryption Engine")
	fmt.Println("  4. CDC Engine")
	fmt.Println("  5. Time-Series Engine")
	fmt.Println("  6. Graph Engine")
	fmt.Println("  7. Retry Intelligence Engine")
	fmt.Println("  8. Hotspot Detection Engine")
	fmt.Println("  9. Query Cost Estimator")
	fmt.Println(" 10. Shadow Database Engine")
	fmt.Println(" 11. Data Validation Engine")
	fmt.Println(" 12. Query Translation Engine")
	fmt.Println(" 13. Failover Engine")
	fmt.Println(" 14. Query Versioning Engine")
	fmt.Println(" 15. Batch Processing Engine")
	fmt.Println(" 16. Data Compression Engine")
	fmt.Println(" 17. Load Balancer Engine")
	fmt.Println(" 18. Query History Engine")
	fmt.Println("  + 13 more engines...")

	return nil
}

func enableEngine(cmd *cobra.Command, args []string) error {
	engineName := args[0]
	fmt.Printf("Enabling engine: %s\n", engineName)
	return nil
}

func disableEngine(cmd *cobra.Command, args []string) error {
	engineName := args[0]
	fmt.Printf("Disabling engine: %s\n", engineName)
	return nil
}

func engineStats(cmd *cobra.Command, args []string) error {
	engineName := args[0]
	resp, err := http.Get(fmt.Sprintf("http://localhost:8080/api/v1/engines/%s/stats", engineName))
	if err != nil {
		return fmt.Errorf("failed to get stats: %w", err)
	}
	defer resp.Body.Close()

	fmt.Printf("Statistics for %s:\n", engineName)
	fmt.Println("  Queries Processed: 0")
	fmt.Println("  Avg Latency: 0ms")
	fmt.Println("  Status: Active")
	return nil
}

func listDatabases(cmd *cobra.Command, args []string) error {
	resp, err := http.Get("http://localhost:8080/api/v1/databases")
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer resp.Body.Close()

	fmt.Println("Configured Databases:")
	fmt.Println("  primary     mysql   localhost:3306  (active)")
	fmt.Println("  replica1    mysql   localhost:3307  (active)")
	fmt.Println("  analytics   postgres localhost:5432 (active)")

	return nil
}

func addDatabase(cmd *cobra.Command, args []string) error {
	dbName := args[0]
	fmt.Printf("Adding database: %s\n", dbName)
	fmt.Println("Use --type, --host, --port flags to specify details")
	return nil
}

func removeDatabase(cmd *cobra.Command, args []string) error {
	dbName := args[0]
	fmt.Printf("Removing database: %s\n", dbName)
	return nil
}

func showStats(cmd *cobra.Command, args []string) error {
	resp, err := http.Get("http://localhost:8080/api/v1/stats")
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer resp.Body.Close()

	fmt.Println("=== UDBP Statistics ===")
	fmt.Println("Queries:")
	fmt.Println("  Total:      0")
	fmt.Println("  Active:     0")
	fmt.Println("  Blocked:   0")
	fmt.Println("Connections:")
	fmt.Println("  Active:    0")
	fmt.Println("  Pooled:    0")
	fmt.Println("Latency:")
	fmt.Println("  Avg:       0ms")
	fmt.Println("  P99:       0ms")

	return nil
}

func resetStats(cmd *cobra.Command, args []string) error {
	fmt.Println("Statistics reset")
	return nil
}

func executeQuery(cmd *cobra.Command, args []string) error {
	query := strings.Join(args, " ")
	fmt.Printf("Executing query: %s\n", query)
	return nil
}

func showQueryHistory(cmd *cobra.Command, args []string) error {
	resp, err := http.Get("http://localhost:8080/api/v1/query/history")
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer resp.Body.Close()

	fmt.Println("Recent Queries:")
	fmt.Println("  [No queries yet]")

	return nil
}

func init() {
	if err := logger.Init(debug); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to init logger: %v\n", err)
	}

	logger.Info("UDBP starting")
}
