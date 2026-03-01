package apikeysrv

import (
	"context"
	"time"

	"github.com/Abraxas-365/manifesto/pkg/errx"
	"github.com/Abraxas-365/manifesto/pkg/iam/apikey"
	"github.com/Abraxas-365/manifesto/pkg/iam/tenant"
	"github.com/Abraxas-365/manifesto/pkg/iam/user"
	"github.com/Abraxas-365/manifesto/pkg/kernel"
	"github.com/google/uuid"
)

type APIKeyService struct {
	apiKeyRepo apikey.APIKeyRepository
	tenantRepo tenant.TenantRepository
	userRepo   user.UserRepository
}

func NewAPIKeyService(
	apiKeyRepo apikey.APIKeyRepository,
	tenantRepo tenant.TenantRepository,
	userRepo user.UserRepository,
) *APIKeyService {
	return &APIKeyService{
		apiKeyRepo: apiKeyRepo,
		tenantRepo: tenantRepo,
		userRepo:   userRepo,
	}
}

func (s *APIKeyService) CreateAPIKey(
	ctx context.Context,
	tenantID kernel.TenantID,
	creatorID kernel.UserID,
	req apikey.CreateAPIKeyRequest,
) (*apikey.CreateAPIKeyResponse, error) {
	tenantEntity, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if !tenantEntity.IsActive() {
		return nil, tenant.ErrTenantSuspended()
	}

	_, err = s.userRepo.FindByID(ctx, creatorID, tenantID)
	if err != nil {
		return nil, err
	}

	if req.UserID != nil {
		_, err := s.userRepo.FindByID(ctx, *req.UserID, tenantID)
		if err != nil {
			return nil, user.ErrUserNotFound()
		}
	}

	var prefix string
	if req.Environment == "live" {
		prefix = apikey.KeyPrefixLive
	} else {
		prefix = apikey.KeyPrefixTest
	}

	generated, err := apikey.GenerateAPIKey(prefix)
	if err != nil {
		return nil, err
	}

	var expiresAt *time.Time
	if req.ExpiresIn != nil && *req.ExpiresIn > 0 {
		expiration := time.Now().UTC().AddDate(0, 0, *req.ExpiresIn)
		expiresAt = &expiration
	}

	newKey := apikey.APIKey{
		ID:          uuid.NewString(),
		KeyHash:     apikey.HashAPIKey(generated.Key),
		KeyPrefix:   generated.KeyPrefix,
		TenantID:    tenantID,
		UserID:      req.UserID,
		Name:        req.Name,
		Description: req.Description,
		Scopes:      req.Scopes,
		IsActive:    true,
		ExpiresAt:   expiresAt,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	if err := s.apiKeyRepo.Save(ctx, newKey); err != nil {
		return nil, errx.Wrap(err, "failed to save API key", errx.TypeInternal)
	}

	return &apikey.CreateAPIKeyResponse{
		APIKey:    newKey.ToDTO(),
		SecretKey: generated.Key,
		Message:   "⚠️ Save this key securely. It will not be shown again!",
	}, nil
}

func (s *APIKeyService) GetAPIKeyByID(
	ctx context.Context,
	keyID string,
	tenantID kernel.TenantID,
) (*apikey.APIKeyDTO, error) {
	key, err := s.apiKeyRepo.FindByID(ctx, keyID, tenantID)
	if err != nil {
		return nil, apikey.ErrAPIKeyNotFound()
	}

	dto := key.ToDTO()
	return &dto, nil
}

func (s *APIKeyService) GetTenantAPIKeys(
	ctx context.Context,
	tenantID kernel.TenantID,
) (*apikey.APIKeyListResponse, error) {
	keys, err := s.apiKeyRepo.FindByTenant(ctx, tenantID)
	if err != nil {
		return nil, errx.Wrap(err, "failed to get API keys", errx.TypeInternal)
	}

	dtos := make([]apikey.APIKeyDTO, 0, len(keys))
	for _, key := range keys {
		dtos = append(dtos, key.ToDTO())
	}

	return &apikey.APIKeyListResponse{
		APIKeys: dtos,
		Total:   len(dtos),
	}, nil
}

func (s *APIKeyService) UpdateAPIKey(
	ctx context.Context,
	keyID string,
	tenantID kernel.TenantID,
	req apikey.UpdateAPIKeyRequest,
) (*apikey.APIKeyDTO, error) {
	key, err := s.apiKeyRepo.FindByID(ctx, keyID, tenantID)
	if err != nil {
		return nil, apikey.ErrAPIKeyNotFound()
	}

	if req.Name != nil {
		key.Name = *req.Name
	}
	if req.Description != nil {
		key.Description = *req.Description
	}
	if req.Scopes != nil {
		key.Scopes = req.Scopes
	}
	if req.IsActive != nil {
		key.IsActive = *req.IsActive
	}

	key.UpdatedAt = time.Now().UTC()

	if err := s.apiKeyRepo.Save(ctx, *key); err != nil {
		return nil, errx.Wrap(err, "failed to update API key", errx.TypeInternal)
	}

	dto := key.ToDTO()
	return &dto, nil
}
func (s *APIKeyService) RevokeAPIKey(
	ctx context.Context,
	keyID string,
	tenantID kernel.TenantID,
) error {
	key, err := s.apiKeyRepo.FindByID(ctx, keyID, tenantID)
	if err != nil {
		return apikey.ErrAPIKeyNotFound()
	}

	key.Revoke()
	return s.apiKeyRepo.Save(ctx, *key)
}

func (s *APIKeyService) DeleteAPIKey(
	ctx context.Context,
	keyID string,
	tenantID kernel.TenantID,
) error {
	_, err := s.apiKeyRepo.FindByID(ctx, keyID, tenantID)
	if err != nil {
		return apikey.ErrAPIKeyNotFound()
	}

	return s.apiKeyRepo.Delete(ctx, keyID, tenantID)
}

func (s *APIKeyService) ValidateAPIKey(
	ctx context.Context,
	keyString string,
) (*apikey.APIKey, error) {
	if !apikey.ValidateAPIKeyFormat(keyString) {
		return nil, apikey.ErrAPIKeyInvalid()
	}

	keyHash := apikey.HashAPIKey(keyString)
	key, err := s.apiKeyRepo.FindByHash(ctx, keyHash)
	if err != nil {
		return nil, apikey.ErrAPIKeyNotFound()
	}

	if !key.IsValid() {
		if key.IsExpired() {
			return nil, apikey.ErrAPIKeyExpired()
		}
		return nil, apikey.ErrAPIKeyRevoked()
	}

	go s.apiKeyRepo.UpdateLastUsed(context.Background(), key.ID)

	return key, nil
}
