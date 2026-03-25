package protocols

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	_ "github.com/lib/pq"
	"github.com/udbp/udbproxy/pkg/logger"
	"github.com/udbp/udbproxy/pkg/types"
)

type PostgresHandler struct {
	registry HandlerRegistry
	mu       sync.RWMutex
	pools    map[string]*sql.DB
}

func NewPostgresHandler(registry HandlerRegistry) *PostgresHandler {
	return &PostgresHandler{
		registry: registry,
		pools:    make(map[string]*sql.DB),
	}
}

func (h *PostgresHandler) DatabaseType() types.DatabaseType {
	return types.DatabaseTypePostgres
}

func (h *PostgresHandler) HandleConnection(ctx context.Context, conn net.Conn, cfg *types.DatabaseConfig) error {
	logger.Info("PostgreSQL connection received")

	conn.SetDeadline(time.Now().Add(30 * time.Second))

	buf := make([]byte, 4096)
	_, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("failed to read: %w", err)
	}

	if string(buf[:5]) == "GET /" {
		return h.handleHTTP(conn)
	}

	pool, err := h.getConnectionPool(cfg)
	if err != nil {
		return err
	}

	return h.proxyQuery(ctx, conn, pool, cfg)
}

func (h *PostgresHandler) handleHTTP(conn net.Conn) error {
	response := "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n{\"status\":\"ok\",\"type\":\"postgres-proxy\"}\r\n"
	conn.Write([]byte(response))
	conn.Close()
	return nil
}

func (h *PostgresHandler) getConnectionPool(cfg *types.DatabaseConfig) (*sql.DB, error) {
	h.mu.RLock()
	if db, ok := h.pools[cfg.Name]; ok {
		h.mu.RUnlock()
		return db, nil
	}
	h.mu.RUnlock()

	sslmode := "disable"
	if cfg.SSLMode {
		sslmode = "require"
	}

	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s connect_timeout=10",
		cfg.Host,
		cfg.Port,
		cfg.Username,
		cfg.Password,
		cfg.Database,
		sslmode,
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if cfg.Pool != nil {
		db.SetMaxOpenConns(cfg.Pool.MaxConnections)
		db.SetMaxIdleConns(cfg.Pool.MaxConnections / 2)
		db.SetConnMaxLifetime(cfg.Pool.MaxLifetime)
		db.SetConnMaxIdleTime(cfg.Pool.MaxIdleTime)
	} else {
		db.SetMaxOpenConns(25)
		db.SetMaxIdleConns(10)
		db.SetConnMaxLifetime(5 * time.Minute)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	h.mu.Lock()
	h.pools[cfg.Name] = db
	h.mu.Unlock()

	return db, nil
}

func (h *PostgresHandler) proxyQuery(ctx context.Context, clientConn net.Conn, pool *sql.DB, cfg *types.DatabaseConfig) error {
	clientConn.SetDeadline(time.Now().Add(60 * time.Second))

	queryBuf := make([]byte, 16384)
	n, err := clientConn.Read(queryBuf)
	if err != nil {
		return fmt.Errorf("failed to read query: %w", err)
	}

	query := string(queryBuf[:n])
	query = strings.TrimSpace(query)

	if query == "" {
		clientConn.Write([]byte("OK"))
		return nil
	}

	rows, err := pool.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("failed to get columns: %w", err)
	}

	result := fmt.Sprintf("Columns: %v\n", columns)
	clientConn.Write([]byte(result))
	return nil
}

func (h *PostgresHandler) ParseQuery(data []byte) (string, error) {
	query := string(data)
	query = strings.TrimSpace(query)
	query = strings.Trim(query, ";")
	return query, nil
}

func (h *PostgresHandler) GetCapabilities() map[string]interface{} {
	return map[string]interface{}{
		"protocol_version":  3,
		"server_version":    "14.0",
		"supports_prepared": true,
		"supports_pipeline": true,
	}
}

func (h *PostgresHandler) GetPool(dbName string) (*sql.DB, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	db, ok := h.pools[dbName]
	return db, ok
}

func (h *PostgresHandler) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, db := range h.pools {
		db.Close()
	}
	h.pools = make(map[string]*sql.DB)
}

func (h *PostgresHandler) HandleTLSConnection(conn net.Conn, tlsConfig *TLSConfig) error {
	cert, err := tls.LoadX509KeyPair(tlsConfig.CertFile, tlsConfig.KeyFile)
	if err != nil {
		return fmt.Errorf("failed to load TLS certificates: %w", err)
	}

	tlsConn := tls.Server(conn, &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	})

	if err := tlsConn.Handshake(); err != nil {
		return fmt.Errorf("TLS handshake failed: %w", err)
	}

	return nil
}
