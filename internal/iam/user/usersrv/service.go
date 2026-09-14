package usersrv

import (
	"context"
	"time"

	"github.com/Abraxas-365/manifesto/internal/errx"
	"github.com/Abraxas-365/manifesto/internal/iam/scopes"
	"github.com/Abraxas-365/manifesto/internal/iam/tenant"
	"github.com/Abraxas-365/manifesto/internal/iam/user"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"github.com/google/uuid"
)

// UserService provides business operations for users
type UserService struct {
	userRepo    user.UserRepository
	tenantRepo  tenant.TenantRepository
	passwordSvc user.PasswordService
}

// NewUserService creates a new user service instance
func NewUserService(
	userRepo user.UserRepository,
	tenantRepo tenant.TenantRepository,
	passwordSvc user.PasswordService,
) *UserService {
	return &UserService{
		userRepo:    userRepo,
		tenantRepo:  tenantRepo,
		passwordSvc: passwordSvc,
	}
}

// CreateUser creates a new user
func (s *UserService) CreateUser(ctx context.Context, req user.CreateUserRequest) (*user.User, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Validate that the tenant exists and is active
	tenantEntity, err := s.tenantRepo.FindByID(ctx, req.TenantID)
	if err != nil {
		return nil, tenant.ErrTenantNotFound()
	}

	if !tenantEntity.IsActive() {
		return nil, tenant.ErrTenantSuspended()
	}

	// Verify the tenant can add more users
	if !tenantEntity.CanAddUser() {
		return nil, tenant.ErrMaxUsersReached()
	}

	// Verify no user exists with the same email
	exists, err := s.userRepo.ExistsByEmail(ctx, req.Email, req.TenantID)
	if err != nil {
		return nil, errx.Wrap(err, "failed to check email existence", errx.TypeInternal)
	}
	if exists {
		return nil, user.ErrUserAlreadyExists()
	}

	// Determine scopes
	scopes, err := s.resolveScopes(req)
	if err != nil {
		return nil, err
	}

	// Validate scopes
	if err := s.validateScopes(scopes, nil); err != nil {
		return nil, err
	}

	// Create new user
	newUser := &user.User{
		ID:            kernel.NewUserID(uuid.NewString()),
		TenantID:      req.TenantID,
		Email:         req.Email,
		Name:          req.Name,
		Status:        user.UserStatusPending, // Pending until onboarding is completed
		Scopes:        scopes,
		EmailVerified: false, // Will be verified later
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	// Save user
	if err := s.userRepo.Save(ctx, *newUser); err != nil {
		return nil, errx.Wrap(err, "failed to save user", errx.TypeInternal)
	}

	// Increment tenant user counter (best-effort — not transactional with user save)
	if err := tenantEntity.AddUser(); err == nil {
		if err := s.tenantRepo.Save(ctx, *tenantEntity); err != nil {
			_ = err // user created successfully, counter drift is non-fatal
		}
	}

	return newUser, nil
}

// GetUserByID gets a user by ID
func (s *UserService) GetUserByID(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID) (*user.UserResponse, error) {
	userEntity, err := s.userRepo.FindByID(ctx, userID, tenantID)
	if err != nil {
		return nil, user.ErrUserNotFound()
	}

	return &user.UserResponse{
		User: *userEntity,
	}, nil
}

// GetUserByEmail gets a user by email
func (s *UserService) GetUserByEmail(ctx context.Context, email string, tenantID kernel.TenantID) (*user.UserResponse, error) {
	userEntity, err := s.userRepo.FindByEmail(ctx, email, tenantID)
	if err != nil {
		return nil, user.ErrUserNotFound()
	}

	return &user.UserResponse{
		User: *userEntity,
	}, nil
}

// GetUsersByTenant gets all users for a tenant
func (s *UserService) GetUsersByTenant(ctx context.Context, tenantID kernel.TenantID) (*user.UserListResponse, error) {
	users, err := s.userRepo.FindByTenant(ctx, tenantID)
	if err != nil {
		return nil, errx.Wrap(err, "failed to get users by tenant", errx.TypeInternal)
	}

	var userResponses []user.UserResponse
	for _, u := range users {
		userResponses = append(userResponses, user.UserResponse{
			User: *u,
		})
	}

	return &user.UserListResponse{
		Users: userResponses,
		Total: len(userResponses),
	}, nil
}

// UpdateUser updates a user
func (s *UserService) UpdateUser(ctx context.Context, userID kernel.UserID, req user.UpdateUserRequest) (*user.User, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	userEntity, err := s.userRepo.FindByID(ctx, userID, req.TenantID)
	if err != nil {
		return nil, user.ErrUserNotFound()
	}

	// Update fields if provided
	if req.Name != nil {
		userEntity.Name = *req.Name
	}

	// Update scopes if provided
	if req.Scopes != nil {
		if len(req.Scopes) > 0 {
			if err := s.validateScopes(req.Scopes, nil); err != nil {
				return nil, err
			}
		}
		userEntity.SetScopes(req.Scopes)
	}

	userEntity.UpdatedAt = time.Now()

	// Save changes
	if err := s.userRepo.Save(ctx, *userEntity); err != nil {
		return nil, errx.Wrap(err, "failed to update user", errx.TypeInternal)
	}

	return userEntity, nil
}

// ReinstateUser restores a suspended, verified member, never a pending signup.
func (s *UserService) ReinstateUser(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID) error {
	u, err := s.userRepo.FindByID(ctx, userID, tenantID)
	if err != nil {
		return err
	}
	if u.Status != user.UserStatusSuspended || !u.EmailVerified {
		return user.ErrInvalidStatus()
	}
	u.Status = user.UserStatusActive
	u.UpdatedAt = time.Now().UTC()
	return s.userRepo.Save(ctx, *u)
}

// SuspendUser suspends a user
func (s *UserService) SuspendUser(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID, reason string) error {
	userEntity, err := s.userRepo.FindByID(ctx, userID, tenantID)
	if err != nil {
		return user.ErrUserNotFound()
	}

	if err := userEntity.Suspend(reason); err != nil {
		return err
	}

	return s.userRepo.Save(ctx, *userEntity)
}

// DeleteUser deletes a user
func (s *UserService) DeleteUser(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID) error {
	// Verify that the user exists
	_, err := s.userRepo.FindByID(ctx, userID, tenantID)
	if err != nil {
		return user.ErrUserNotFound()
	}

	// Delete user
	if err := s.userRepo.Delete(ctx, userID, tenantID); err != nil {
		return errx.Wrap(err, "failed to delete user", errx.TypeInternal)
	}

	return nil
}

// ============================================================================
// Scope Management Methods
// ============================================================================

// AddScopesToUser adds scopes to a user.
// callerScopes is retained for API compatibility; it cannot grant platform authority.
func (s *UserService) AddScopesToUser(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID, newScopes []string, callerScopes []string) error {
	req := user.ChangeUserScopesRequest{Scopes: newScopes}
	if err := req.Validate(); err != nil {
		return err
	}

	userEntity, err := s.userRepo.FindByID(ctx, userID, tenantID)
	if err != nil {
		return user.ErrUserNotFound()
	}

	// Validate scopes
	if err := s.validateScopes(newScopes, callerScopes); err != nil {
		return err
	}

	// Add scopes (avoiding duplicates)
	for _, scope := range newScopes {
		if !userEntity.HasScope(scope) {
			userEntity.AddScope(scope)
		}
	}

	return s.userRepo.Save(ctx, *userEntity)
}

// RemoveScopesFromUser removes scopes from a user
func (s *UserService) RemoveScopesFromUser(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID, scopeList []string) error {
	// Revocation must also allow cleaning up legacy or retired scopes.
	req := user.ChangeUserScopesRequest{Scopes: scopeList}
	if err := req.Validate(); err != nil {
		return err
	}

	userEntity, err := s.userRepo.FindByID(ctx, userID, tenantID)
	if err != nil {
		return user.ErrUserNotFound()
	}

	for _, scope := range scopeList {
		userEntity.RemoveScope(scope)
	}

	return s.userRepo.Save(ctx, *userEntity)
}

// SetUserScopes sets the scopes for a user (replaces existing ones).
// callerScopes is retained for API compatibility; it cannot grant platform authority.
func (s *UserService) SetUserScopes(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID, newScopes []string, callerScopes []string) error {
	req := user.SetUserScopesRequest{Scopes: newScopes}
	if err := req.Validate(); err != nil {
		return err
	}

	userEntity, err := s.userRepo.FindByID(ctx, userID, tenantID)
	if err != nil {
		return user.ErrUserNotFound()
	}

	// An explicit empty array revokes all direct scopes, not role-derived scopes.
	if len(newScopes) > 0 {
		if err := s.validateScopes(newScopes, callerScopes); err != nil {
			return err
		}
	}

	userEntity.SetScopes(newScopes)
	return s.userRepo.Save(ctx, *userEntity)
}

// GetUserScopes retrieves the scopes for a user
func (s *UserService) GetUserScopes(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID) (*user.UserScopesResponse, error) {
	userEntity, err := s.userRepo.FindByID(ctx, userID, tenantID)
	if err != nil {
		return nil, user.ErrUserNotFound()
	}

	// Build detailed scope information
	scopeDetails := make([]user.ScopeDetail, 0, len(userEntity.Scopes))
	for _, scope := range userEntity.Scopes {
		scopeDetails = append(scopeDetails, user.ScopeDetail{
			Name:        scope,
			Description: scopes.GetScopeDescription(scope),
			Category:    scopes.GetScopeCategory(scope),
		})
	}

	return &user.UserScopesResponse{
		UserID:       userID,
		Scopes:       userEntity.Scopes,
		ScopeDetails: scopeDetails,
		TotalScopes:  len(userEntity.Scopes),
	}, nil
}

// ============================================================================
// Private Helper Methods
// ============================================================================

// resolveScopes determines the final scopes based on the request
func (s *UserService) resolveScopes(req user.CreateUserRequest) ([]string, error) {
	if len(req.Scopes) > 0 {
		return req.Scopes, nil
	}
	return []string{}, nil
}

// validateScopes accepts only tenant application scopes, regardless of caller authority.
func (s *UserService) validateScopes(scopesl []string, callerScopes []string) error {
	if len(scopesl) == 0 {
		return user.ErrInvalidScopes().WithDetail("reason", "at least one scope is required")
	}

	// Platform permissions are never assignable through tenant IAM.
	if scopes.ContainsPlatformScope(scopesl) {
		return user.ErrInvalidScopes().
			WithDetail("reason", "platform scopes are not available in tenant IAM")
	}

	// Validate each scope
	invalidScopes := []string{}
	for _, scope := range scopesl {
		if !scopes.ValidateScope(scope) {
			invalidScopes = append(invalidScopes, scope)
		}
	}

	if len(invalidScopes) > 0 {
		return user.ErrInvalidScopes().
			WithDetail("invalid_scopes", invalidScopes).
			WithDetail("hint", "Use GetAllAvailableScopes() to see valid scopes")
	}

	return nil
}
