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
	userEntity, err := s.userRepo.FindByID(ctx, userID, req.TenantID)
	if err != nil {
		return nil, user.ErrUserNotFound()
	}

	// Update fields if provided
	if req.Name != nil {
		userEntity.Name = *req.Name
	}

	if req.Status != nil {
		switch *req.Status {
		case user.UserStatusActive:
			if err := userEntity.Activate(); err != nil {
				return nil, err
			}
		case user.UserStatusSuspended:
			if err := userEntity.Suspend("Updated by admin"); err != nil {
				return nil, err
			}
		}
	}

	// Update scopes if provided
	if req.Scopes != nil && len(req.Scopes) > 0 {
		if err := s.validateScopes(req.Scopes, nil); err != nil {
			return nil, err
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

// ActivateUser activates a pending user
func (s *UserService) ActivateUser(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID) error {
	userEntity, err := s.userRepo.FindByID(ctx, userID, tenantID)
	if err != nil {
		return user.ErrUserNotFound()
	}

	if err := userEntity.Activate(); err != nil {
		return err
	}

	return s.userRepo.Save(ctx, *userEntity)
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

	// Decrement tenant user counter (best-effort — not transactional with user delete)
	if tenantEntity, err := s.tenantRepo.FindByID(ctx, tenantID); err == nil {
		tenantEntity.RemoveUser()
		if err := s.tenantRepo.Save(ctx, *tenantEntity); err != nil {
			_ = err // user deleted successfully, counter drift is non-fatal
		}
	}

	return nil
}

// ============================================================================
// Scope Management Methods
// ============================================================================

// AddScopesToUser adds scopes to a user.
// callerScopes are the effective scopes of the authenticated caller, used to
// enforce that only platform operators can assign platform-reserved scopes.
func (s *UserService) AddScopesToUser(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID, newScopes []string, callerScopes []string) error {
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
	if err := s.validateScopes(scopeList, nil); err != nil {
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
// callerScopes are the effective scopes of the authenticated caller, used to
// enforce that only platform operators can assign platform-reserved scopes.
func (s *UserService) SetUserScopes(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID, newScopes []string, callerScopes []string) error {
	userEntity, err := s.userRepo.FindByID(ctx, userID, tenantID)
	if err != nil {
		return user.ErrUserNotFound()
	}

	// Validate scopes
	if err := s.validateScopes(newScopes, callerScopes); err != nil {
		return err
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

// validateScopes validates that the scopes are valid and that the caller is
// authorized to assign them. Platform scopes (platform:*) require the caller
// to hold a platform scope themselves.
func (s *UserService) validateScopes(scopesl []string, callerScopes []string) error {
	if len(scopesl) == 0 {
		return user.ErrInvalidScopes().WithDetail("reason", "at least one scope is required")
	}

	// Reject platform scopes from non-platform callers
	if scopes.ContainsPlatformScope(scopesl) && !scopes.CallerHasPlatformScope(callerScopes) {
		return user.ErrInvalidScopes().
			WithDetail("reason", "platform scopes can only be assigned by platform administrators")
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
