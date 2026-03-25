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

	_ "github.com/go-sql-driver/mysql"
	"github.com/udbp/udbproxy/pkg/logger"
	"github.com/udbp/udbproxy/pkg/types"
)

type MySQLHandler struct {
	registry HandlerRegistry
	mu       sync.RWMutex
	pools    map[string]*sql.DB
}

func NewMySQLHandler(registry HandlerRegistry) *MySQLHandler {
	return &MySQLHandler{
		registry: registry,
		pools:    make(map[string]*sql.DB),
	}
}

func (h *MySQLHandler) DatabaseType() types.DatabaseType {
	return types.DatabaseTypeMySQL
}

func (h *MySQLHandler) HandleConnection(ctx context.Context, conn net.Conn, cfg *types.DatabaseConfig) error {
	logger.Info("MySQL connection received")

	conn.SetDeadline(time.Now().Add(30 * time.Second))

	buf := make([]byte, 4096)
	_, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("failed to read initial handshake: %w", err)
	}

	if string(buf[:5]) == "GET /" {
		return h.handleHTTP(conn, cfg)
	}

	pool, err := h.getConnectionPool(cfg)
	if err != nil {
		return err
	}

	return h.proxyQuery(ctx, conn, pool, cfg)
}

func (h *MySQLHandler) handleHTTP(conn net.Conn, cfg *types.DatabaseConfig) error {
	response := "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n{\"status\":\"ok\",\"type\":\"mysql-proxy\"}\r\n"
	conn.Write([]byte(response))
	conn.Close()
	return nil
}

func (h *MySQLHandler) getConnectionPool(cfg *types.DatabaseConfig) (*sql.DB, error) {
	h.mu.RLock()
	if db, ok := h.pools[cfg.Name]; ok {
		h.mu.RUnlock()
		return db, nil
	}
	h.mu.RUnlock()

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&timeout=10s",
		cfg.Username,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.Database,
	)

	if cfg.SSLMode {
		dsn += "&tls=skip-verify"
	}

	db, err := sql.Open("mysql", dsn)
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

func (h *MySQLHandler) proxyQuery(ctx context.Context, clientConn net.Conn, pool *sql.DB, cfg *types.DatabaseConfig) error {
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
	for rows.Next() {
		break
	}

	clientConn.Write([]byte(result))
	return nil
}

func (h *MySQLHandler) ParseQuery(data []byte) (string, error) {
	query := string(data)
	query = strings.TrimSpace(query)
	query = strings.Trim(query, ";")
	return query, nil
}

func (h *MySQLHandler) GetCapabilities() map[string]interface{} {
	return map[string]interface{}{
		"protocol_version":    10,
		"server_version":      "5.7.0",
		"supports_exceptions": true,
		"supports_set_guess":  true,
	}
}

func (h *MySQLHandler) GetPool(dbName string) (*sql.DB, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	db, ok := h.pools[dbName]
	return db, ok
}

func (h *MySQLHandler) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, db := range h.pools {
		db.Close()
	}
	h.pools = make(map[string]*sql.DB)
}

type TLSConfig struct {
	CertFile string
	KeyFile  string
}

func (h *MySQLHandler) HandleTLSConnection(conn net.Conn, tlsConfig *TLSConfig) error {
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
