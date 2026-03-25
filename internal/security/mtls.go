package security

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

type TLSConfig struct {
	CertFile           string
	KeyFile            string
	CAFile             string
	ClientAuth         tls.ClientAuthType
	MinVersion         uint16
	MaxVersion         uint16
	VerifyPeerCert     bool
	ServerName         string
	InsecureSkipVerify bool
}

type MTLSManager struct {
	mu               sync.RWMutex
	serverTLSConfig  *tls.Config
	clientTLSConfigs map[string]*tls.Config
	certPool         *x509.CertPool
}

func NewMTLSManager() *MTLSManager {
	return &MTLSManager{
		clientTLSConfigs: make(map[string]*tls.Config),
	}
}

func (m *MTLSManager) LoadServerTLSConfig(config *TLSConfig) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load server certificate: %w", err)
	}

	serverConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   config.MinVersion,
		MaxVersion:   config.MaxVersion,
		ClientAuth:   config.ClientAuth,
	}

	if config.CAFile != "" {
		caCert, err := loadCAConfig(config.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load CA certificate: %w", err)
		}
		serverConfig.ClientCAs = caCert
	}

	m.mu.Lock()
	m.serverTLSConfig = serverConfig
	m.mu.Unlock()

	return serverConfig, nil
}

func (m *MTLSManager) GetServerTLSConfig() *tls.Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.serverTLSConfig
}

func (m *MTLSManager) LoadClientTLSConfig(name string, config *TLSConfig) (*tls.Config, error) {
	var cert []tls.Certificate
	if config.CertFile != "" && config.KeyFile != "" {
		c, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		cert = []tls.Certificate{c}
	}

	tlsConfig := &tls.Config{
		Certificates:       cert,
		MinVersion:         config.MinVersion,
		MaxVersion:         config.MaxVersion,
		ServerName:         config.ServerName,
		InsecureSkipVerify: config.InsecureSkipVerify,
	}

	if config.CAFile != "" {
		caCert, err := loadCAConfig(config.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load CA certificate: %w", err)
		}
		tlsConfig.RootCAs = caCert
		tlsConfig.ClientCAs = caCert
	}

	m.mu.Lock()
	m.clientTLSConfigs[name] = tlsConfig
	m.mu.Unlock()

	return tlsConfig, nil
}

func (m *MTLSManager) GetClientTLSConfig(name string) (*tls.Config, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	config, ok := m.clientTLSConfigs[name]
	return config, ok
}

func (m *MTLSManager) UpdateClientTLSConfig(name string, config *tls.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clientTLSConfigs[name] = config
}

func (m *MTLSManager) RemoveClientTLSConfig(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.clientTLSConfigs, name)
}

func loadCAConfig(caFile string) (*x509.CertPool, error) {
	caCertPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, err
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCertPEM) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	return pool, nil
}

func CreateListener(addr string, tlsConfig *tls.Config) (net.Listener, error) {
	if tlsConfig == nil {
		return net.Listen("tcp", addr)
	}

	listener, err := tls.Listen("tcp", addr, tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create TLS listener: %w", err)
	}

	return listener, nil
}

func DialWithTLS(network, addr string, tlsConfig *tls.Config) (net.Conn, error) {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
	}

	if tlsConfig == nil {
		return dialer.Dial(network, addr)
	}

	return tls.DialWithDialer(dialer, network, addr, tlsConfig)
}

type TLSConnectionState struct {
	Protocol         string
	CipherSuite      string
	ServerName       string
	PeerCertificates []string
}

func GetConnectionState(conn net.Conn) (*TLSConnectionState, bool) {
	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return nil, false
	}

	state := tlsConn.ConnectionState()
	proto := getProtocolName(state.Version)
	cipher := getCipherSuiteName(state.CipherSuite)

	certs := make([]string, len(state.PeerCertificates))
	for i, cert := range state.PeerCertificates {
		certs[i] = cert.Subject.CommonName
	}

	return &TLSConnectionState{
		Protocol:         proto,
		CipherSuite:      cipher,
		ServerName:       state.ServerName,
		PeerCertificates: certs,
	}, true
}

func getProtocolName(version uint16) string {
	switch version {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return "Unknown"
	}
}

func getCipherSuiteName(id uint16) string {
	suites := map[uint16]string{
		0x002F: "TLS_RSA_WITH_AES_128_CBC_SHA",
		0x0035: "TLS_RSA_WITH_AES_256_CBC_SHA",
		0x003C: "TLS_RSA_WITH_AES_128_CBC_SHA256",
		0x009D: "TLS_RSA_WITH_AES_256_GCM_SHA384",
		0xC007: "TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA",
		0xC008: "TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA",
		0xC009: "TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256",
		0xC00A: "TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384",
		0xC013: "TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA",
		0xC014: "TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA",
		0xC023: "TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256",
		0xC024: "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
		0xC02F: "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
		0xC030: "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
		0x1301: "TLS_AES_128_GCM_SHA256",
		0x1302: "TLS_AES_256_GCM_SHA384",
		0x1303: "TLS_CHACHA20_POLY1305_SHA256",
	}
	return suites[id]
}

const (
	TLSVersion12 = tls.VersionTLS12
	TLSVersion13 = tls.VersionTLS13
)
