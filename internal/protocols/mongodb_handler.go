package protocols

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/udbp/udbproxy/pkg/logger"
	"github.com/udbp/udbproxy/pkg/types"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MongoDBHandler struct {
	registry HandlerRegistry
	mu       sync.RWMutex
	pools    map[string]*mongo.Database
	client   *mongo.Client
}

func NewMongoDBHandler(registry HandlerRegistry) *MongoDBHandler {
	return &MongoDBHandler{
		registry: registry,
		pools:    make(map[string]*mongo.Database),
	}
}

func (h *MongoDBHandler) DatabaseType() types.DatabaseType {
	return types.DatabaseTypeMongoDB
}

func (h *MongoDBHandler) HandleConnection(ctx context.Context, conn net.Conn, cfg *types.DatabaseConfig) error {
	logger.Info("MongoDB connection received")

	conn.SetDeadline(time.Now().Add(30 * time.Second))

	buf := make([]byte, 4096)
	_, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("failed to read: %w", err)
	}

	if string(buf[:5]) == "GET /" {
		return h.handleHTTP(conn, cfg)
	}

	db, err := h.getDatabase(cfg)
	if err != nil {
		return err
	}

	return h.proxyCommand(ctx, conn, db, cfg)
}

func (h *MongoDBHandler) handleHTTP(conn net.Conn, cfg *types.DatabaseConfig) error {
	response := "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n{\"status\":\"ok\",\"type\":\"mongodb-proxy\"}\r\n"
	conn.Write([]byte(response))
	conn.Close()
	return nil
}

func (h *MongoDBHandler) getDatabase(cfg *types.DatabaseConfig) (*mongo.Database, error) {
	h.mu.RLock()
	if db, ok := h.pools[cfg.Name]; ok {
		h.mu.RUnlock()
		return db, nil
	}
	h.mu.RUnlock()

	uri := fmt.Sprintf("mongodb://%s:%s@%s:%d/%s?authSource=admin&timeout=10s",
		cfg.Username,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.Database,
	)

	clientOpts := options.Client().ApplyURI(uri)
	if cfg.SSLMode {
		clientOpts.SetTLSConfig(nil)
	}

	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		client.Disconnect(ctx)
		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	h.mu.Lock()
	if h.client == nil {
		h.client = client
	}
	h.pools[cfg.Name] = client.Database(cfg.Database)
	h.mu.Unlock()

	return h.pools[cfg.Name], nil
}

func (h *MongoDBHandler) proxyCommand(ctx context.Context, clientConn net.Conn, db *mongo.Database, cfg *types.DatabaseConfig) error {
	clientConn.SetDeadline(time.Now().Add(60 * time.Second))

	queryBuf := make([]byte, 16384)
	n, err := clientConn.Read(queryBuf)
	if err != nil {
		return fmt.Errorf("failed to read command: %w", err)
	}

	command := string(queryBuf[:n])
	command = command[:len(command)-1]

	result := fmt.Sprintf("OK: %s\n", command)
	clientConn.Write([]byte(result))
	return nil
}

func (h *MongoDBHandler) ParseQuery(data []byte) (string, error) {
	return string(data), nil
}

func (h *MongoDBHandler) GetCapabilities() map[string]interface{} {
	return map[string]interface{}{
		"protocol_version":  1,
		"server_version":    "6.0",
		"supports_sharding": true,
		"supports_retry":    true,
	}
}

func (h *MongoDBHandler) GetPool(dbName string) (*mongo.Database, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	db, ok := h.pools[dbName]
	return db, ok
}

func (h *MongoDBHandler) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.client != nil {
		h.client.Disconnect(ctx)
	}
	h.pools = make(map[string]*mongo.Database)
}

var ctx = context.Background()
