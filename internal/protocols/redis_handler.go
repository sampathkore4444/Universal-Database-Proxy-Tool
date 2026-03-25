package protocols

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/udbp/udbproxy/pkg/logger"
	"github.com/udbp/udbproxy/pkg/types"
)

type RedisHandler struct {
	registry HandlerRegistry
	mu       sync.RWMutex
	clients  map[string]*redis.Client
}

func NewRedisHandler(registry HandlerRegistry) *RedisHandler {
	return &RedisHandler{
		registry: registry,
		clients:  make(map[string]*redis.Client),
	}
}

func (h *RedisHandler) DatabaseType() types.DatabaseType {
	return types.DatabaseTypeRedis
}

func (h *RedisHandler) HandleConnection(ctx context.Context, conn net.Conn, cfg *types.DatabaseConfig) error {
	logger.Info("Redis connection received")

	conn.SetDeadline(time.Now().Add(30 * time.Second))

	buf := make([]byte, 4096)
	_, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("failed to read: %w", err)
	}

	if string(buf[:5]) == "GET /" {
		return h.handleHTTP(conn, cfg)
	}

	client, err := h.getClient(cfg)
	if err != nil {
		return err
	}

	return h.proxyCommand(ctx, conn, client, cfg)
}

func (h *RedisHandler) handleHTTP(conn net.Conn, cfg *types.DatabaseConfig) error {
	response := "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n{\"status\":\"ok\",\"type\":\"redis-proxy\"}\r\n"
	conn.Write([]byte(response))
	conn.Close()
	return nil
}

func (h *RedisHandler) getClient(cfg *types.DatabaseConfig) (*redis.Client, error) {
	h.mu.RLock()
	if client, ok := h.clients[cfg.Name]; ok {
		h.mu.RUnlock()
		return client, nil
	}
	h.mu.RUnlock()

	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password: cfg.Password,
		DB:       0,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	h.mu.Lock()
	h.clients[cfg.Name] = client
	h.mu.Unlock()

	return client, nil
}

func (h *RedisHandler) proxyCommand(ctx context.Context, clientConn net.Conn, client *redis.Client, cfg *types.DatabaseConfig) error {
	clientConn.SetDeadline(time.Now().Add(60 * time.Second))

	queryBuf := make([]byte, 16384)
	n, err := clientConn.Read(queryBuf)
	if err != nil {
		return fmt.Errorf("failed to read command: %w", err)
	}

	command := string(queryBuf[:n])
	command = strings.TrimSpace(command)

	parts := strings.Fields(command)
	if len(parts) == 0 {
		clientConn.Write([]byte("+OK\r\n"))
		return nil
	}

	cmd := strings.ToUpper(parts[0])
	args := parts[1:]

	var result string
	switch cmd {
	case "PING":
		result = "+PONG\r\n"
	case "GET":
		if len(args) > 0 {
			val, err := client.Get(ctx, args[0]).Result()
			if err == redis.Nil {
				result = "$-1\r\n"
			} else if err != nil {
				result = "-ERR " + err.Error() + "\r\n"
			} else {
				result = fmt.Sprintf("$%d\r\n%s\r\n", len(val), val)
			}
		} else {
			result = "-ERR wrong number of arguments\r\n"
		}
	case "SET":
		if len(args) >= 2 {
			err := client.Set(ctx, args[0], args[1], 0).Err()
			if err != nil {
				result = "-ERR " + err.Error() + "\r\n"
			} else {
				result = "+OK\r\n"
			}
		} else {
			result = "-ERR wrong number of arguments\r\n"
		}
	case "DEL":
		if len(args) > 0 {
			n, err := client.Del(ctx, args...).Result()
			if err != nil {
				result = "-ERR " + err.Error() + "\r\n"
			} else {
				result = ":" + strconv.FormatInt(n, 10) + "\r\n"
			}
		} else {
			result = ":0\r\n"
		}
	case "KEYS":
		if len(args) > 0 {
			keys, err := client.Keys(ctx, args[0]).Result()
			if err != nil {
				result = "-ERR " + err.Error() + "\r\n"
			} else {
				result = "*" + strconv.Itoa(len(keys)) + "\r\n"
				for _, k := range keys {
					result += fmt.Sprintf("$%d\r\n%s\r\n", len(k), k)
				}
			}
		} else {
			result = "*0\r\n"
		}
	default:
		result = "+OK\r\n"
	}

	clientConn.Write([]byte(result))
	return nil
}

func (h *RedisHandler) ParseQuery(data []byte) (string, error) {
	return string(data), nil
}

func (h *RedisHandler) GetCapabilities() map[string]interface{} {
	return map[string]interface{}{
		"protocol_version": "resp3",
		"server_version":   "7.0",
		"supports_streams": true,
		"supports_cluster": true,
	}
}

func (h *RedisHandler) GetClient(name string) (*redis.Client, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	client, ok := h.clients[name]
	return client, ok
}

func (h *RedisHandler) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, client := range h.clients {
		client.Close()
	}
	h.clients = make(map[string]*redis.Client)
}
