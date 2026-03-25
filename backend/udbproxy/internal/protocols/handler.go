package protocols

import (
	"context"
	"net"

	"github.com/udbp/udbproxy/pkg/types"
)

type ProtocolHandler interface {
	DatabaseType() types.DatabaseType
	HandleConnection(ctx context.Context, conn net.Conn, cfg *types.DatabaseConfig) error
	ParseQuery(data []byte) (string, error)
	GetCapabilities() map[string]interface{}
}

type HandlerRegistry map[types.DatabaseType]ProtocolHandler

func NewHandlerRegistry() HandlerRegistry {
	return make(HandlerRegistry)
}

func (r HandlerRegistry) Register(dbType types.DatabaseType, handler ProtocolHandler) {
	r[dbType] = handler
}

func (r HandlerRegistry) Get(dbType types.DatabaseType) (ProtocolHandler, bool) {
	handler, ok := r[dbType]
	return handler, ok
}
