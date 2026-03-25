package main

import (
	"fmt"
	"net/http"
	"os"

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
- Smart engines (Security, Observability, Caching)
- Read/write splitting and routing
- SQL injection detection
- Query result caching
- Observability with Prometheus metrics

Usage:
  udbproxy serve --config config.yaml`,
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

func init() {
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "config.yaml", "Path to configuration file")
	rootCmd.PersistentFlags().BoolVarP(&debug, "debug", "d", false, "Enable debug logging")

	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(healthCmd)
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

func init() {
	if err := logger.Init(debug); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to init logger: %v\n", err)
	}

	logger.Info("UDBP starting")
}
