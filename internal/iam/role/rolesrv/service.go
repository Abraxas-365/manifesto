package rolesrv

import (
	"context"
	"time"

	"github.com/Abraxas-365/manifesto/internal/errx"
	"github.com/Abraxas-365/manifesto/internal/iam/role"
	"github.com/Abraxas-365/manifesto/internal/iam/scopes"
	"github.com/Abraxas-365/manifesto/internal/iam/tenant"
	"github.com/Abraxas-365/manifesto/internal/iam/user"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"github.com/google/uuid"
)

type RoleService struct {
	roleRepo   role.RoleRepository
	tenantRepo tenant.TenantRepository
	userRepo   user.UserRepository
}

func NewRoleService(
	roleRepo role.RoleRepository,
	tenantRepo tenant.TenantRepository,
	userRepo user.UserRepository,
) *RoleService {
	return &RoleService{
		roleRepo:   roleRepo,
		tenantRepo: tenantRepo,
		userRepo:   userRepo,
	}
}

func (s *RoleService) CreateRole(
	ctx context.Context,
	tenantID kernel.TenantID,
	req role.CreateRoleRequest,
) (*role.Role, error) {
	tenantEntity, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if !tenantEntity.IsActive() {
		return nil, tenant.ErrTenantSuspended()
	}

	// Check for duplicate name
	existing, _ := s.roleRepo.FindByName(ctx, req.Name, tenantID)
	if existing != nil {
		return nil, role.ErrRoleAlreadyExists().WithDetail("name", req.Name)
	}

	if err := s.validateScopes(req.Scopes); err != nil {
		return nil, err
	}

	newRole := &role.Role{
		ID:          kernel.NewRoleID(uuid.NewString()),
		TenantID:    tenantID,
		Name:        req.Name,
		Description: req.Description,
		Scopes:      req.Scopes,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	if err := s.roleRepo.Save(ctx, *newRole); err != nil {
		return nil, errx.Wrap(err, "failed to save role", errx.TypeInternal)
	}

	return newRole, nil
}

func (s *RoleService) GetRoleByID(
	ctx context.Context,
	roleID kernel.RoleID,
	tenantID kernel.TenantID,
) (*role.RoleDTO, error) {
	r, err := s.roleRepo.FindByID(ctx, roleID, tenantID)
	if err != nil {
		return nil, role.ErrRoleNotFound()
	}
	dto := r.ToDTO()
	return &dto, nil
}

func (s *RoleService) GetTenantRoles(
	ctx context.Context,
	tenantID kernel.TenantID,
) (*role.RoleListResponse, error) {
	roles, err := s.roleRepo.FindByTenant(ctx, tenantID)
	if err != nil {
		return nil, errx.Wrap(err, "failed to get roles", errx.TypeInternal)
	}

	dtos := make([]role.RoleDTO, 0, len(roles))
	for _, r := range roles {
		dtos = append(dtos, r.ToDTO())
	}

	return &role.RoleListResponse{
		Roles: dtos,
		Total: len(dtos),
	}, nil
}

func (s *RoleService) UpdateRole(
	ctx context.Context,
	roleID kernel.RoleID,
	tenantID kernel.TenantID,
	req role.UpdateRoleRequest,
) (*role.RoleDTO, error) {
	r, err := s.roleRepo.FindByID(ctx, roleID, tenantID)
	if err != nil {
		return nil, role.ErrRoleNotFound()
	}

	if req.Name != nil {
		// Check for duplicate name (exclude current role)
		existing, _ := s.roleRepo.FindByName(ctx, *req.Name, tenantID)
		if existing != nil && existing.ID != r.ID {
			return nil, role.ErrRoleAlreadyExists().WithDetail("name", *req.Name)
		}
		r.Name = *req.Name
	}
	if req.Description != nil {
		r.Description = *req.Description
	}
	if req.Scopes != nil {
		if err := s.validateScopes(req.Scopes); err != nil {
			return nil, err
		}
		r.SetScopes(req.Scopes)
	}

	r.UpdatedAt = time.Now().UTC()

	if err := s.roleRepo.Save(ctx, *r); err != nil {
		return nil, errx.Wrap(err, "failed to update role", errx.TypeInternal)
	}

	dto := r.ToDTO()
	return &dto, nil
}

func (s *RoleService) DeleteRole(
	ctx context.Context,
	roleID kernel.RoleID,
	tenantID kernel.TenantID,
) error {
	_, err := s.roleRepo.FindByID(ctx, roleID, tenantID)
	if err != nil {
		return role.ErrRoleNotFound()
	}

	return s.roleRepo.Delete(ctx, roleID, tenantID)
}

// AssignRoleToUser assigns a role to a user
func (s *RoleService) AssignRoleToUser(
	ctx context.Context,
	roleID kernel.RoleID,
	userID kernel.UserID,
	tenantID kernel.TenantID,
) error {
	// Verify role exists
	_, err := s.roleRepo.FindByID(ctx, roleID, tenantID)
	if err != nil {
		return role.ErrRoleNotFound()
	}

	// Verify user exists
	_, err = s.userRepo.FindByID(ctx, userID, tenantID)
	if err != nil {
		return user.ErrUserNotFound()
	}

	userRole := role.UserRole{
		UserID:     userID,
		RoleID:     roleID,
		TenantID:   tenantID,
		AssignedAt: time.Now().UTC(),
	}

	return s.roleRepo.AssignToUser(ctx, userRole)
}

// UnassignRoleFromUser removes a role from a user
func (s *RoleService) UnassignRoleFromUser(
	ctx context.Context,
	roleID kernel.RoleID,
	userID kernel.UserID,
	tenantID kernel.TenantID,
) error {
	return s.roleRepo.UnassignFromUser(ctx, userID, roleID, tenantID)
}

// GetUserRoles returns all roles assigned to a user with effective scopes
func (s *RoleService) GetUserRoles(
	ctx context.Context,
	userID kernel.UserID,
	tenantID kernel.TenantID,
) (*role.UserRolesResponse, error) {
	userEntity, err := s.userRepo.FindByID(ctx, userID, tenantID)
	if err != nil {
		return nil, user.ErrUserNotFound()
	}

	roles, err := s.roleRepo.FindByUser(ctx, userID, tenantID)
	if err != nil {
		return nil, errx.Wrap(err, "failed to get user roles", errx.TypeInternal)
	}

	dtos := make([]role.RoleDTO, 0, len(roles))
	for _, r := range roles {
		dtos = append(dtos, r.ToDTO())
	}

	effectiveScopes := s.resolveEffectiveScopes(userEntity.Scopes, roles)

	return &role.UserRolesResponse{
		UserID:          userID,
		Roles:           dtos,
		DirectScopes:    userEntity.Scopes,
		EffectiveScopes: effectiveScopes,
	}, nil
}

// GetEffectiveScopes resolves the complete set of scopes for a user:
// direct user scopes + all scopes from assigned roles (deduplicated)
func (s *RoleService) GetEffectiveScopes(
	ctx context.Context,
	userID kernel.UserID,
	tenantID kernel.TenantID,
) ([]string, error) {
	userEntity, err := s.userRepo.FindByID(ctx, userID, tenantID)
	if err != nil {
		return nil, err
	}

	roles, err := s.roleRepo.FindByUser(ctx, userID, tenantID)
	if err != nil {
		return nil, err
	}

	return s.resolveEffectiveScopes(userEntity.Scopes, roles), nil
}

func (s *RoleService) resolveEffectiveScopes(directScopes []string, roles []*role.Role) []string {
	seen := make(map[string]struct{}, len(directScopes))
	result := make([]string, 0, len(directScopes))

	// Add direct scopes first
	for _, scope := range directScopes {
		if _, ok := seen[scope]; !ok {
			seen[scope] = struct{}{}
			result = append(result, scope)
		}
	}

	// Add role scopes (deduplicated)
	for _, r := range roles {
		for _, scope := range r.Scopes {
			if _, ok := seen[scope]; !ok {
				seen[scope] = struct{}{}
				result = append(result, scope)
			}
		}
	}

	return result
}

func (s *RoleService) validateScopes(scopesList []string) error {
	if len(scopesList) == 0 {
		return role.ErrRoleInvalidScopes().WithDetail("reason", "at least one scope is required")
	}

	var invalidScopes []string
	for _, scope := range scopesList {
		if !scopes.ValidateScope(scope) {
			invalidScopes = append(invalidScopes, scope)
		}
	}

	if len(invalidScopes) > 0 {
		return role.ErrRoleInvalidScopes().
			WithDetail("invalid_scopes", invalidScopes).
			WithDetail("hint", "Use scopes.GetAllScopes() to see valid options")
	}

	return nil
}
