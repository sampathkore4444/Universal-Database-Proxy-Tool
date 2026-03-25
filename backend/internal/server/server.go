package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/udbp/udbproxy/config"
	"github.com/udbp/udbproxy/internal/engines"
	"github.com/udbp/udbproxy/internal/protocols"
	"github.com/udbp/udbproxy/internal/routing"
	"github.com/udbp/udbproxy/pkg/logger"
	"github.com/udbp/udbproxy/pkg/metrics"
	"github.com/udbp/udbproxy/pkg/types"
)

type ProxyServer struct {
	config        *config.Config
	httpServer    *http.Server
	listener      net.Listener
	handler       *RequestHandler
	metricsServer *http.Server
	wg            sync.WaitGroup
	ctx           context.Context
	cancel        context.CancelFunc
}

type RequestHandler struct {
	registry       protocols.HandlerRegistry
	router         *routing.Router
	enginePipeline *engines.EnginePipeline
	config         *config.Config
}

func NewProxyServer(cfg *config.Config) (*ProxyServer, error) {
	ctx, cancel := context.WithCancel(context.Background())

	server := &ProxyServer{
		config: cfg,
		ctx:    ctx,
		cancel: cancel,
	}

	if err := server.init(); err != nil {
		cancel()
		return nil, err
	}

	return server, nil
}

func (s *ProxyServer) init() error {
	if err := logger.Init(s.config.Logging.Level == "debug"); err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}

	if err := metrics.Init(s.ctx); err != nil {
		logger.Warn("Failed to initialize metrics")
	}

	s.handler = &RequestHandler{
		registry: protocols.NewHandlerRegistry(),
		config:   s.config,
	}

	s.registerProtocols()
	s.initRouter()
	s.initEngines()

	return nil
}

func (s *ProxyServer) registerProtocols() {
	mysqlHandler := protocols.NewMySQLHandler(s.handler.registry)
	postgresHandler := protocols.NewPostgresHandler(s.handler.registry)

	s.handler.registry.Register(types.DatabaseTypeMySQL, mysqlHandler)
	s.handler.registry.Register(types.DatabaseTypePostgres, postgresHandler)
}

func (s *ProxyServer) initRouter() {
	var rules []types.RoutingRule
	for _, r := range s.config.RoutingRules {
		rules = append(rules, types.RoutingRule{
			Name:          r.Name,
			MatchPattern:  r.MatchPattern,
			Database:      r.Database,
			IsReadReplica: r.IsReadReplica,
			Priority:      r.Priority,
		})
	}

	s.handler.router = routing.NewRouter(rules, routing.StrategyRoundRobin)

	for _, db := range s.config.Databases {
		s.handler.router.RegisterDatabase(&types.DatabaseConfig{
			Name:          db.Name,
			Type:          types.DatabaseType(db.Type),
			Host:          db.Host,
			Port:          db.Port,
			Database:      db.Database,
			Username:      db.Username,
			Password:      db.Password,
			SSLMode:       db.SSLMode,
			IsReadReplica: db.IsReadReplica,
		})
	}
}

func (s *ProxyServer) initEngines() {
	obsConfig := &types.ObservabilityConfig{
		Enabled:            s.config.Observability.Enabled,
		LogQueries:         s.config.Observability.LogQueries,
		LogResponses:       s.config.Observability.LogResponses,
		SlowQueryThreshold: time.Duration(s.config.Observability.SlowQueryThreshold) * time.Millisecond,
		MetricsEnabled:     s.config.Observability.MetricsEnabled,
		TracingEnabled:     s.config.Observability.TracingEnabled,
	}

	var securityRules []types.SecurityRule
	for _, r := range s.config.SecurityRules {
		securityRules = append(securityRules, types.SecurityRule{
			Name:         r.Name,
			MatchPattern: r.MatchPattern,
			Action:       types.SecurityAction(r.Action),
			MaskFields:   r.MaskFields,
			LogOnly:      r.LogOnly,
		})
	}

	securityEngine := engines.NewSecurityEngine(securityRules)
	obsEngine := engines.NewObservabilityEngine(obsConfig)

	cacheConfig := &types.CacheConfig{
		Enabled:   s.config.Cache.Enabled,
		Backend:   s.config.Cache.Backend,
		TTL:       time.Duration(s.config.Cache.TTL) * time.Second,
		MaxSize:   s.config.Cache.MaxSize,
		KeyPrefix: s.config.Cache.KeyPrefix,
	}
	cacheEngine := engines.NewCachingEngine(cacheConfig)

	s.handler.enginePipeline = engines.NewEnginePipeline(
		securityEngine,
		obsEngine,
		cacheEngine,
	)
}

func (s *ProxyServer) Start() error {
	addr := fmt.Sprintf("%s:%d", s.config.Server.ListenAddress, s.config.Server.ListenPort)

	var err error
	s.listener, err = net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	logger.Info("Server starting")

	s.wg.Add(1)
	go s.serve()

	if s.config.Metrics.Enabled {
		s.wg.Add(1)
		go s.startMetricsServer()
	}

	return nil
}

func (s *ProxyServer) serve() {
	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				logger.Error("Failed to accept connection")
				continue
			}
		}

		s.wg.Add(1)
		go s.handleConnection(conn)
	}
}

func (s *ProxyServer) handleConnection(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	s.handler.ServeConn(s.ctx, conn)
}

func (s *ProxyServer) startMetricsServer() {
	defer s.wg.Done()

	addr := fmt.Sprintf(":%d", s.config.Metrics.Port)
	s.metricsServer = &http.Server{
		Addr:    addr,
		Handler: metrics.GetHandler(),
	}

	logger.Info("Metrics server starting")

	if err := s.metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("Metrics server failed")
	}
}

func (s *ProxyServer) Stop() error {
	logger.Info("Server stopping")

	s.cancel()

	if s.listener != nil {
		s.listener.Close()
	}

	if s.metricsServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.metricsServer.Shutdown(shutdownCtx)
	}

	s.wg.Wait()

	logger.Info("Server stopped")
	logger.Sync()

	return nil
}

func (s *ProxyServer) WaitForInterrupt() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan

	logger.Info("Received shutdown signal")
}

func (h *RequestHandler) ServeConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(30 * time.Second))

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return
	}

	protocol := detectProtocol(buf[:n])
	if protocol == "" {
		h.handleHTTP(conn)
		return
	}

	handler, ok := h.registry.Get(protocol)
	if !ok {
		logger.Error("No handler for protocol")
		conn.Write([]byte("ERROR: unsupported protocol"))
		return
	}

	dbType := handler.DatabaseType()

	var targetDB *types.DatabaseConfig
	if dbType == types.DatabaseTypeMySQL {
		for _, db := range h.config.Databases {
			if db.Type == "mysql" {
				targetDB = &types.DatabaseConfig{
					Name:     db.Name,
					Type:     types.DatabaseTypeMySQL,
					Host:     db.Host,
					Port:     db.Port,
					Database: db.Database,
					Username: db.Username,
					Password: db.Password,
					SSLMode:  db.SSLMode,
				}
				break
			}
		}
	} else if dbType == types.DatabaseTypePostgres {
		for _, db := range h.config.Databases {
			if db.Type == "postgres" {
				targetDB = &types.DatabaseConfig{
					Name:     db.Name,
					Type:     types.DatabaseTypePostgres,
					Host:     db.Host,
					Port:     db.Port,
					Database: db.Database,
					Username: db.Username,
					Password: db.Password,
					SSLMode:  db.SSLMode,
				}
				break
			}
		}
	}

	if targetDB == nil {
		conn.Write([]byte("ERROR: no database configured"))
		return
	}

	if err := handler.HandleConnection(ctx, conn, targetDB); err != nil {
		logger.Error("Handler error")
	}
}

func (h *RequestHandler) handleHTTP(conn net.Conn) {
	response := "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n{\"status\":\"ok\",\"service\":\"udbproxy\"}\r\n"
	conn.Write([]byte(response))
}

func detectProtocol(data []byte) types.DatabaseType {
	if len(data) < 5 {
		return ""
	}

	if string(data[:5]) == "GET /" || string(data[:5]) == "POST" || string(data[:5]) == "HEAD" {
		return ""
	}

	if data[0] == 0x16 && len(data) >= 5 {
		if data[1] == 0x03 && (data[2] >= 0x01 && data[2] <= 0x03) {
			return ""
		}
	}

	if len(data) >= 3 {
		if data[0] == 0x16 || data[0] == 0x17 {
			return ""
		}
	}

	if string(data[:3]) == "\x00\x00\x01" || string(data[:3]) == "\x00\x00\x03" {
		return types.DatabaseTypePostgres
	}

	if data[0] == 0x10 || (data[0] >= 0x01 && data[0] <= 0x0f) {
		return types.DatabaseTypeMySQL
	}

	return types.DatabaseTypeMySQL
}

func Run() error {
	cfg := config.LoadDefault()
	server, err := NewProxyServer(cfg)
	if err != nil {
		return err
	}

	if err := server.Start(); err != nil {
		return err
	}

	server.WaitForInterrupt()

	return server.Stop()
}
