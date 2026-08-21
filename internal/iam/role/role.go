package role

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Abraxas-365/manifesto/internal/errx"
	"github.com/Abraxas-365/manifesto/internal/kernel"
)

// Role represents a named collection of scopes that can be assigned to users.
// Similar to AWS IAM managed policies attached to roles.
type Role struct {
	ID          string          `db:"id" json:"id"`
	TenantID    kernel.TenantID `db:"tenant_id" json:"tenant_id"`
	Name        string          `db:"name" json:"name"`
	Description string          `db:"description" json:"description,omitempty"`
	Scopes      []string        `db:"scopes" json:"scopes"`
	CreatedAt   time.Time       `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time       `db:"updated_at" json:"updated_at"`
}

// HasScope checks if the role contains a specific scope
func (r *Role) HasScope(scope string) bool {
	return kernel.ScopesContain(r.Scopes, scope)
}

// SetScopes replaces the role's scopes
func (r *Role) SetScopes(scopes []string) {
	r.Scopes = scopes
	r.UpdatedAt = time.Now()
}

// UserRole represents the assignment of a role to a user
type UserRole struct {
	UserID     kernel.UserID   `db:"user_id" json:"user_id"`
	RoleID     string          `db:"role_id" json:"role_id"`
	TenantID   kernel.TenantID `db:"tenant_id" json:"tenant_id"`
	AssignedAt time.Time       `db:"assigned_at" json:"assigned_at"`
}

// ============================================================================
// DTOs
// ============================================================================

type RoleDTO struct {
	ID          string          `json:"id"`
	TenantID    kernel.TenantID `json:"tenant_id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Scopes      []string        `json:"scopes"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

func (r *Role) ToDTO() RoleDTO {
	return RoleDTO{
		ID:          r.ID,
		TenantID:    r.TenantID,
		Name:        r.Name,
		Description: r.Description,
		Scopes:      r.Scopes,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

type CreateRoleRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Scopes      []string `json:"scopes"`
}

func (r *CreateRoleRequest) Validate() error {
	if utf8.RuneCountInString(strings.TrimSpace(r.Name)) < 2 {
		return errx.Validation("Name is required and must be at least 2 characters").WithDetail("field", "name")
	}
	if len(r.Scopes) == 0 {
		return errx.Validation("At least one scope is required").WithDetail("field", "scopes")
	}
	return nil
}

type UpdateRoleRequest struct {
	Name        *string  `json:"name,omitempty"`
	Description *string  `json:"description,omitempty"`
	Scopes      []string `json:"scopes,omitempty"`
}

func (r *UpdateRoleRequest) Validate() error {
	if r.Name != nil && utf8.RuneCountInString(strings.TrimSpace(*r.Name)) < 2 {
		return errx.Validation("Name must be at least 2 characters").WithDetail("field", "name")
	}
	return nil
}

type RoleListResponse struct {
	Roles []RoleDTO `json:"roles"`
	Total int       `json:"total"`
}

type AssignRoleRequest struct {
	UserID kernel.UserID `json:"user_id"`
}

func (r *AssignRoleRequest) Validate() error {
	if r.UserID.IsEmpty() {
		return errx.Validation("User ID is required").WithDetail("field", "user_id")
	}
	return nil
}

type UserRolesResponse struct {
	UserID          kernel.UserID `json:"user_id"`
	Roles           []RoleDTO     `json:"roles"`
	DirectScopes    []string      `json:"direct_scopes"`
	EffectiveScopes []string      `json:"effective_scopes"`
}

// ============================================================================
// Error Registry
// ============================================================================

var ErrRegistry = errx.NewRegistry("ROLE")

var (
	CodeRoleNotFound        = ErrRegistry.Register("NOT_FOUND", errx.TypeNotFound, http.StatusNotFound, "Role not found")
	CodeRoleAlreadyExists   = ErrRegistry.Register("ALREADY_EXISTS", errx.TypeConflict, http.StatusConflict, "Role with this name already exists in tenant")
	CodeRoleInvalidScopes   = ErrRegistry.Register("INVALID_SCOPES", errx.TypeValidation, http.StatusBadRequest, "Invalid scopes provided")
	CodeRoleAlreadyAssigned = ErrRegistry.Register("ALREADY_ASSIGNED", errx.TypeConflict, http.StatusConflict, "Role already assigned to user")
	CodeRoleNotAssigned     = ErrRegistry.Register("NOT_ASSIGNED", errx.TypeNotFound, http.StatusNotFound, "Role not assigned to user")
)

func ErrRoleNotFound() *errx.Error {
	return ErrRegistry.New(CodeRoleNotFound)
}

func ErrRoleAlreadyExists() *errx.Error {
	return ErrRegistry.New(CodeRoleAlreadyExists)
}

func ErrRoleInvalidScopes() *errx.Error {
	return ErrRegistry.New(CodeRoleInvalidScopes)
}

func ErrRoleAlreadyAssigned() *errx.Error {
	return ErrRegistry.New(CodeRoleAlreadyAssigned)
}

func ErrRoleNotAssigned() *errx.Error {
	return ErrRegistry.New(CodeRoleNotAssigned)
}
