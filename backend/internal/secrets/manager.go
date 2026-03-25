package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type SecretsManager interface {
	GetSecret(path string) (string, error)
	SetSecret(path, value string) error
	DeleteSecret(path string) error
	ListSecrets(path string) ([]string, error)
}

type VaultConfig struct {
	Address    string
	Token      string
	PathPrefix string
	Timeout    time.Duration
	MaxRetries int
	CertFile   string
	Insecure   bool
}

type VaultClient struct {
	config    *VaultConfig
	client    *http.Client
	mu        sync.RWMutex
	secrets   map[string]string
	providers []SecretsProvider
}

type SecretsProvider interface {
	GetSecret(path string) (string, error)
	SetSecret(path, value string) error
}

type EnvProvider struct{}

func (e *EnvProvider) GetSecret(path string) (string, error) {
	key := strings.ReplaceAll(path, "/", "_")
	key = strings.ToUpper(key)
	return os.Getenv(key), nil
}

func (e *EnvProvider) SetSecret(path, value string) error {
	return fmt.Errorf("environment provider is read-only")
}

type FileProvider struct {
	filePath string
}

func (fp *FileProvider) GetSecret(path string) (string, error) {
	data, err := os.ReadFile(fp.filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read secrets file: %w", err)
	}

	var secrets map[string]string
	if err := json.Unmarshal(data, &secrets); err != nil {
		return "", fmt.Errorf("failed to parse secrets file: %w", err)
	}

	if value, ok := secrets[path]; ok {
		return value, nil
	}

	return "", fmt.Errorf("secret not found: %s", path)
}

func (fp *FileProvider) SetSecret(path, value string) error {
	return fmt.Errorf("file provider is read-only")
}

func NewVaultClient(config *VaultConfig) *VaultClient {
	client := &http.Client{
		Timeout: config.Timeout,
		Transport: &http.Transport{
			MaxIdleConns: 10,
		},
	}

	vc := &VaultClient{
		config:    config,
		client:    client,
		secrets:   make(map[string]string),
		providers: []SecretsProvider{},
	}

	vc.providers = append(vc.providers, &EnvProvider{})

	return vc
}

func (vc *VaultClient) AddProvider(provider SecretsProvider) {
	vc.providers = append(vc.providers, provider)
}

func (vc *VaultClient) GetSecret(path string) (string, error) {
	vc.mu.RLock()
	if value, ok := vc.secrets[path]; ok {
		vc.mu.RUnlock()
		return value, nil
	}
	vc.mu.RUnlock()

	for _, provider := range vc.providers {
		value, err := provider.GetSecret(path)
		if err == nil && value != "" {
			vc.mu.Lock()
			vc.secrets[path] = value
			vc.mu.Unlock()
			return value, nil
		}
	}

	if vc.config != nil && vc.config.Address != "" {
		return vc.getFromVault(path)
	}

	return "", fmt.Errorf("secret not found: %s", path)
}

func (vc *VaultClient) SetSecret(path, value string) error {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	vc.secrets[path] = value
	return nil
}

func (vc *VaultClient) DeleteSecret(path string) error {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	delete(vc.secrets, path)
	return nil
}

func (vc *VaultClient) ListSecrets(path string) ([]string, error) {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	var secrets []string
	for key := range vc.secrets {
		if strings.HasPrefix(key, path) {
			secrets = append(secrets, key)
		}
	}
	return secrets, nil
}

func (vc *VaultClient) getFromVault(path string) (string, error) {
	fullPath := vc.config.PathPrefix + "/" + path

	req, err := http.NewRequest("GET", vc.config.Address+"/v1/"+fullPath, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-Vault-Token", vc.config.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := vc.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to connect to Vault: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Vault returned status: %d", resp.StatusCode)
	}

	var result struct {
		Data struct {
			Data map[string]string `json:"data"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to parse Vault response: %w", err)
	}

	for key, value := range result.Data.Data {
		vc.secrets[path+"/"+key] = value
		return value, nil
	}

	return "", fmt.Errorf("secret not found in Vault: %s", path)
}

func (vc *VaultClient) SetVaultToken(token string) {
	vc.config.Token = token
}

func (vc *VaultClient) GetVaultToken() string {
	return vc.config.Token
}

type DatabaseCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
}

func (vc *VaultClient) GetDatabaseCredentials(path string) (*DatabaseCredentials, error) {
	secretPath := path + "/config"
	data, err := vc.GetSecret(secretPath)
	if err != nil {
		return nil, err
	}

	var creds DatabaseCredentials
	if err := json.Unmarshal([]byte(data), &creds); err != nil {
		return nil, fmt.Errorf("failed to parse database credentials: %w", err)
	}

	return &creds, nil
}

func (vc *VaultClient) SetDatabaseCredentials(path string, creds *DatabaseCredentials) error {
	data, err := json.Marshal(creds)
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}

	return vc.SetSecret(path+"/config", string(data))
}

type SecretsWatcher struct {
	client   *VaultClient
	interval time.Duration
	paths    []string
	callback func(path, value string)
	stopChan chan struct{}
}

func NewSecretsWatcher(client *VaultClient, interval time.Duration) *SecretsWatcher {
	return &SecretsWatcher{
		client:   client,
		interval: interval,
		paths:    []string{},
		stopChan: make(chan struct{}),
	}
}

func (sw *SecretsWatcher) AddPath(path string) {
	sw.paths = append(sw.paths, path)
}

func (sw *SecretsWatcher) SetCallback(callback func(path, value string)) {
	sw.callback = callback
}

func (sw *SecretsWatcher) Start(ctx context.Context) {
	ticker := time.NewTicker(sw.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sw.checkSecrets()
		case <-sw.stopChan:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (sw *SecretsWatcher) checkSecrets() {
	for _, path := range sw.paths {
		value, err := sw.client.GetSecret(path)
		if err == nil && sw.callback != nil {
			sw.callback(path, value)
		}
	}
}

func (sw *SecretsWatcher) Stop() {
	close(sw.stopChan)
}

type StaticSecretsManager struct {
	mu      sync.RWMutex
	secrets map[string]string
}

func NewStaticSecretsManager() *StaticSecretsManager {
	return &StaticSecretsManager{
		secrets: make(map[string]string),
	}
}

func (sm *StaticSecretsManager) GetSecret(path string) (string, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	value, ok := sm.secrets[path]
	if !ok {
		return "", fmt.Errorf("secret not found: %s", path)
	}
	return value, nil
}

func (sm *StaticSecretsManager) SetSecret(path, value string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.secrets[path] = value
	return nil
}

func (sm *StaticSecretsManager) DeleteSecret(path string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.secrets, path)
	return nil
}

func (sm *StaticSecretsManager) ListSecrets(path string) ([]string, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var secrets []string
	for key := range sm.secrets {
		if strings.HasPrefix(key, path) {
			secrets = append(secrets, key)
		}
	}
	return secrets, nil
}
