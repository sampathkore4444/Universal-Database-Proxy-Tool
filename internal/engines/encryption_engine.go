package engines

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"

	"github.com/udbp/udbproxy/pkg/types"
)

// EncryptionEngine handles column-level encryption, tokenization, and masking
type EncryptionEngine struct {
	BaseEngine
	config      *EncryptionConfig
	columnRules map[string]*EncryptionRule // table.column -> rule
	keyStore    KeyStore
	stats       *EncryptionStats
	mu          sync.RWMutex
}

type EncryptionConfig struct {
	Enabled         bool
	EnableTokenization bool
	EnableMasking   bool
	KeyRotationDays int
}

type EncryptionRule struct {
	Table         string
	Column        string
	Encryption    EncryptionType
	KeyID         string
	MaskPattern   string // For partial masking
	Tokenize      bool
	TokenFormat   string // Format for tokenized values
}

type EncryptionType string

const (
	EncryptionTypeNone       EncryptionType = "none"
	EncryptionTypeAES256    EncryptionType = "aes256"
	EncryptionTypeTokenized EncryptionType = "tokenized"
	EncryptionTypeMasked    EncryptionType = "masked"
	EncryptionTypeHashed    EncryptionType = "hashed"
)

type KeyStore interface {
	GetKey(keyID string) ([]byte, error)
	RotateKey(keyID string) error
}

type KeyStoreImpl struct {
	keys map[string][]byte
	mu   sync.RWMutex
}

func (ks *KeyStoreImpl) GetKey(keyID string) ([]byte, error) {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	key, ok := ks.keys[keyID]
	if !ok {
		return nil, fmt.Errorf("key not found: %s", keyID)
	}
	return key, nil
}

func (ks *KeyStoreImpl) RotateKey(keyID string) error {
	// Generate new key logic here
	return nil
}

type EncryptionStats struct {
	EncryptedQueries   int64
	TokenizedQueries   int64
	MaskedQueries      int64
	KeyRotations       int64
	EncryptionErrors   int64
	mu                 sync.RWMutex
}

// NewEncryptionEngine creates a new Encryption Engine
func NewEncryptionEngine(config *EncryptionConfig) *EncryptionEngine {
	if config == nil {
		config = &EncryptionConfig{
			Enabled:            true,
			EnableTokenization: true,
			EnableMasking:     true,
			KeyRotationDays:   90,
		}
	}

	engine := &EncryptionEngine{
		BaseEngine:  BaseEngine{name: "encryption"},
		config:      config,
		columnRules: make(map[string]*EncryptionRule),
		keyStore:    &KeyStoreImpl{keys: make(map[string][]byte)},
		stats:       &EncryptionStats{},
	}

	// Initialize default keys
	engine.initDefaultKeys()

	return engine
}

func (e *EncryptionEngine) initDefaultKeys() {
	// Generate a default 256-bit key
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err == nil {
		e.keyStore.(*KeyStoreImpl).keys["default"] = key
	}
}

// AddEncryptionRule adds an encryption rule for a column
func (e *EncryptionEngine) AddEncryptionRule(rule *EncryptionRule) {
	key := fmt.Sprintf("%s.%s", rule.Table, rule.Column)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.columnRules[key] = rule
}

// Process handles query encryption
func (e *EncryptionEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	query := strings.TrimSpace(qc.RawQuery)
	if query == "" {
		return types.EngineResult{Continue: true}
	}

	// Check for encrypted columns in query
	rules := e.findApplicableRules(query)
	
	if len(rules) > 0 {
		e.stats.mu.Lock()
		e.stats.EncryptedQueries++
		e.stats.mu.Unlock()

		// Mark query as requiring encryption handling
		if qc.Metadata == nil {
			qc.Metadata = make(map[string]interface{})
		}
		qc.Metadata["encryption_required"] = true
		qc.Metadata["encrypted_columns"] = rules
	}

	return types.EngineResult{Continue: true}
}

// ProcessResponse handles response encryption
func (e *EncryptionEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled || qc.Response == nil || qc.Response.Data == nil {
		return types.EngineResult{Continue: true}
	}

	// Decrypt response data
	if encrypted, ok := qc.Metadata["encryption_required"].(bool); ok && encrypted {
		if rules, ok := qc.Metadata["encrypted_columns"].([]*EncryptionRule); ok {
			e.decryptResponse(qc.Response, rules)
		}
	}

	return types.EngineResult{Continue: true}
}

// findApplicableRules finds encryption rules that apply to the query
func (e *EncryptionEngine) findApplicableRules(query string) []*EncryptionRule {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var applicable []*EncryptionRule
	
	upperQuery := strings.ToUpper(query)
	
	for key, rule := range e.columnRules {
		tablePattern := fmt.Sprintf("(?i)%s", rule.Table)
		columnPattern := fmt.Sprintf("(?i)%s\\s+", rule.Column)
		
		if regexp.MustCompile(tablePattern).MatchString(upperQuery) &&
			regexp.MustCompile(columnPattern).MatchString(upperQuery) {
			applicable = append(applicable, rule)
		}
	}
	
	return applicable
}

// decryptResponse decrypts response data based on rules
func (e *EncryptionEngine) decryptResponse(response *types.QueryResponse, rules []*EncryptionRule) {
	if response.Data == nil {
		return
	}

	for _, rule := range rules {
		if rule.Encryption == EncryptionTypeAES256 {
			e.decryptColumn(response.Data, rule.Column)
		} else if rule.Encryption == EncryptionTypeTokenized {
			e.detokenizeColumn(response.Data, rule.Column)
		} else if rule.Encryption == EncryptionTypeMasked {
			e.maskColumn(response.Data, rule.Column, rule.MaskPattern)
		}
	}
}

// decryptColumn decrypts a specific column in the result set
func (e *EncryptionEngine) decryptColumn(data [][]interface{}, column string) {
	// In a real implementation, this would decrypt specific columns
	// Simplified for demonstration
}

// detokenizeColumn detokenizes a specific column
func (e *EncryptionEngine) detokenizeColumn(data [][]interface{}, column string) {
	// In a real implementation, this would replace tokens with actual values
}

// maskColumn masks a specific column
func (e *EncryptionEngine) maskColumn(data [][]interface{}, column string, pattern string) {
	// Apply masking pattern
}

// EncryptValue encrypts a single value
func (e *EncryptionEngine) EncryptValue(plaintext string, keyID string) (string, error) {
	key, err := e.keyStore.GetKey(keyID)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	ciphertext := make([]byte, aes.BlockSize+len(plaintext))
	iv := ciphertext[:aes.BlockSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}

	stream := cipher.NewCFBEncrypter(block, iv)
	stream.XORKeyStream(ciphertext[aes.BlockSize:], []byte(plaintext))

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptValue decrypts a single value
func (e *EncryptionEngine) DecryptValue(ciphertext string, keyID string) (string, error) {
	key, err := e.keyStore.GetKey(keyID)
	if err != nil {
		return "", err
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	if len(data) < aes.BlockSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	iv := data[:aes.BlockSize]
	data = data[aes.BlockSize:]

	stream := cipher.NewCFBDecrypter(block, iv)
	stream.XORKeyStream(data, data)

	return string(data), nil
}

// Tokenize generates a token for a value
func (e *EncryptionEngine) Tokenize(value string) string {
	// Generate a random token
	token := make([]byte, 16)
	io.ReadFull(rand.Reader, token)
	return base64.URLEncoding.EncodeToString(token)
}

// GetEncryptionStats returns encryption statistics
func (e *EncryptionEngine) GetEncryptionStats() EncryptionStatsResponse {
	e.stats.mu.RLock()
	defer e.stats.mu.RUnlock()

	return EncryptionStatsResponse{
		EncryptedQueries: e.stats.EncryptedQueries,
		TokenizedQueries: e.stats.TokenizedQueries,
		MaskedQueries:    e.stats.MaskedQueries,
		KeyRotations:    e.stats.KeyRotations,
		EncryptionErrors: e.stats.EncryptionErrors,
	}
}

type EncryptionStatsResponse struct {
	EncryptedQueries int64 `json:"encrypted_queries"`
	TokenizedQueries int64 `json:"tokenized_queries"`
	MaskedQueries    int64 `json:"masked_queries"`
	KeyRotations     int64 `json:"key_rotations"`
	EncryptionErrors int64 `json:"encryption_errors"`
}