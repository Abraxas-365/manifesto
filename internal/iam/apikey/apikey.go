package apikey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Abraxas-365/manifesto/internal/errx"
	"github.com/Abraxas-365/manifesto/internal/kernel"
)

type APIKey struct {
	ID          string          `db:"id" json:"id"`
	KeyHash     string          `db:"key_hash" json:"-"` // Never expose the hash
	KeyPrefix   string          `db:"key_prefix" json:"key_prefix"`
	TenantID    kernel.TenantID `db:"tenant_id" json:"tenant_id"`
	UserID      *kernel.UserID  `db:"user_id" json:"user_id,omitempty"`
	Name        string          `db:"name" json:"name"`
	Description string          `db:"description" json:"description,omitempty"`
	Scopes      []string        `db:"scopes" json:"scopes"`
	IsActive    bool            `db:"is_active" json:"is_active"`
	ExpiresAt   *time.Time      `db:"expires_at" json:"expires_at,omitempty"`
	LastUsedAt  *time.Time      `db:"last_used_at" json:"last_used_at,omitempty"`
	CreatedAt   time.Time       `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time       `db:"updated_at" json:"updated_at"`
}

func (k *APIKey) IsValid() bool {
	if !k.IsActive {
		return false
	}
	if k.ExpiresAt != nil && time.Now().After(*k.ExpiresAt) {
		return false
	}
	return true
}

func (k *APIKey) IsExpired() bool {
	return k.ExpiresAt != nil && time.Now().After(*k.ExpiresAt)
}

func (k *APIKey) HasScope(scope string) bool {
	return kernel.ScopesContain(k.Scopes, scope)
}

func (k *APIKey) Revoke() {
	k.IsActive = false
	k.UpdatedAt = time.Now()
}

func (k *APIKey) UpdateLastUsed() {
	now := time.Now()
	k.LastUsedAt = &now
}

var (
	KeyPrefixLive string = "manifesto_live"
	KeyPrefixTest string = "manifesto_test"
	TokenLength   int    = 32
)

func InitAPIKeyConfig(livePrefix, testPrefix string, tokenLength int) {
	KeyPrefixLive = livePrefix
	KeyPrefixTest = testPrefix
	TokenLength = tokenLength
}

type GeneratedAPIKey struct {
	Key       string
	APIKey    APIKey
	KeyPrefix string
}

func GenerateAPIKey(prefix string) (*GeneratedAPIKey, error) {
	randomBytes := make([]byte, TokenLength)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, errx.Wrap(err, "failed to generate random key", errx.TypeInternal)
	}

	keySecret := hex.EncodeToString(randomBytes)
	fullKey := fmt.Sprintf("%s_%s", prefix, keySecret)

	return &GeneratedAPIKey{
		Key:       fullKey,
		KeyPrefix: fmt.Sprintf("%s_%s...", prefix, keySecret[:8]),
	}, nil
}

func HashAPIKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}

func ValidateAPIKeyFormat(key string) bool {
	parts := strings.SplitN(key, "_", 3)
	if len(parts) != 3 {
		return false
	}

	return (fmt.Sprintf("%s_%s", parts[0], parts[1]) == KeyPrefixLive ||
		fmt.Sprintf("%s_%s", parts[0], parts[1]) == KeyPrefixTest) && len(parts[2]) == 64
}

type APIKeyDTO struct {
	ID          string          `json:"id"`
	KeyPrefix   string          `json:"key_prefix"`
	TenantID    kernel.TenantID `json:"tenant_id"`
	UserID      *kernel.UserID  `json:"user_id,omitempty"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Scopes      []string        `json:"scopes"`
	IsActive    bool            `json:"is_active"`
	ExpiresAt   *time.Time      `json:"expires_at,omitempty"`
	LastUsedAt  *time.Time      `json:"last_used_at,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

func (k *APIKey) ToDTO() APIKeyDTO {
	return APIKeyDTO{
		ID:          k.ID,
		KeyPrefix:   k.KeyPrefix,
		TenantID:    k.TenantID,
		UserID:      k.UserID,
		Name:        k.Name,
		Description: k.Description,
		Scopes:      k.Scopes,
		IsActive:    k.IsActive,
		ExpiresAt:   k.ExpiresAt,
		LastUsedAt:  k.LastUsedAt,
		CreatedAt:   k.CreatedAt,
	}
}

type CreateAPIKeyRequest struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Scopes      []string       `json:"scopes"`
	ExpiresIn   *int           `json:"expires_in"` // Days until expiration
	Environment string         `json:"environment"`
	UserID      *kernel.UserID `json:"user_id"` // Optional: associate with specific user
}

func (r *CreateAPIKeyRequest) Validate() error {
	if utf8.RuneCountInString(strings.TrimSpace(r.Name)) < 3 {
		return errx.Validation("Name is required and must be at least 3 characters").WithDetail("field", "name")
	}
	if len(r.Scopes) == 0 {
		return errx.Validation("At least one scope is required").WithDetail("field", "scopes")
	}
	if r.Environment != "live" && r.Environment != "test" {
		return errx.Validation("Environment must be 'live' or 'test'").WithDetail("field", "environment")
	}
	return nil
}

type CreateAPIKeyResponse struct {
	APIKey    APIKeyDTO `json:"api_key"`
	SecretKey string    `json:"secret_key"` // Only shown once!
	Message   string    `json:"message"`
}

type UpdateAPIKeyRequest struct {
	Name        *string  `json:"name,omitempty"`
	Description *string  `json:"description,omitempty"`
	Scopes      []string `json:"scopes,omitempty"`
	IsActive    *bool    `json:"is_active,omitempty"`
}

func (r *UpdateAPIKeyRequest) Validate() error {
	if r.Name != nil && utf8.RuneCountInString(strings.TrimSpace(*r.Name)) < 3 {
		return errx.Validation("Name must be at least 3 characters").WithDetail("field", "name")
	}
	return nil
}

type APIKeyListResponse struct {
	APIKeys []APIKeyDTO `json:"api_keys"`
	Total   int         `json:"total"`
}

type RevokeAPIKeyRequest struct {
	Reason string `json:"reason"`
}

// ============================================================================
// Error Registry
// ============================================================================

var ErrRegistry = errx.NewRegistry("APIKEY")

var (
	CodeAPIKeyNotFound          = ErrRegistry.Register("NOT_FOUND", errx.TypeNotFound, http.StatusNotFound, "API key not found")
	CodeAPIKeyInvalid           = ErrRegistry.Register("INVALID", errx.TypeAuthorization, http.StatusUnauthorized, "Invalid API key")
	CodeAPIKeyExpired           = ErrRegistry.Register("EXPIRED", errx.TypeAuthorization, http.StatusUnauthorized, "API key expired")
	CodeAPIKeyRevoked           = ErrRegistry.Register("REVOKED", errx.TypeAuthorization, http.StatusUnauthorized, "API key revoked")
	CodeAPIKeyInsufficientScope = ErrRegistry.Register("INSUFFICIENT_SCOPE", errx.TypeAuthorization, http.StatusForbidden, "API key does not have required scope")
	CodeAPIKeyInvalidScopes     = ErrRegistry.Register("INVALID_SCOPES", errx.TypeValidation, http.StatusBadRequest, "Invalid scopes provided")
)

func ErrAPIKeyNotFound() *errx.Error {
	return ErrRegistry.New(CodeAPIKeyNotFound)
}

func ErrAPIKeyInvalid() *errx.Error {
	return ErrRegistry.New(CodeAPIKeyInvalid)
}

func ErrAPIKeyExpired() *errx.Error {
	return ErrRegistry.New(CodeAPIKeyExpired)
}

func ErrAPIKeyRevoked() *errx.Error {
	return ErrRegistry.New(CodeAPIKeyRevoked)
}

func ErrAPIKeyInsufficientScope() *errx.Error {
	return ErrRegistry.New(CodeAPIKeyInsufficientScope)
}

func ErrAPIKeyInvalidScopes() *errx.Error {
	return ErrRegistry.New(CodeAPIKeyInvalidScopes)
}
