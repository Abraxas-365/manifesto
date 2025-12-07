package user

import (
	"net/http"
	"time"

	"github.com/Abraxas-365/manifesto/pkg/errx"
	"github.com/Abraxas-365/manifesto/pkg/iam"
	"github.com/Abraxas-365/manifesto/pkg/kernel"
	"github.com/Abraxas-365/manifesto/pkg/ptrx"
	"slices"
)

// ============================================================================
// User Entity
// ============================================================================

// UserStatus define los posibles estados de un usuario
type UserStatus string

const (
	UserStatusActive    UserStatus = "ACTIVE"
	UserStatusInactive  UserStatus = "INACTIVE"
	UserStatusSuspended UserStatus = "SUSPENDED"
	UserStatusPending   UserStatus = "PENDING" // Invitado pero no completó onboarding
)

// User es la entidad rica que representa a un usuario en el sistema
type User struct {
	ID              kernel.UserID     `db:"id" json:"id"`
	TenantID        kernel.TenantID   `db:"tenant_id" json:"tenant_id"`
	Email           string            `db:"email" json:"email"`
	Name            string            `db:"name" json:"name"`
	Picture         *string           `db:"picture" json:"picture,omitempty"`
	Status          UserStatus        `db:"status" json:"status"`
	Scopes          []string          `db:"scopes" json:"scopes"` // Scope-based permissions
	OAuthProvider   iam.OAuthProvider `db:"oauth_provider" json:"oauth_provider"`
	OAuthProviderID string            `db:"oauth_provider_id" json:"oauth_provider_id"`
	EmailVerified   bool              `db:"email_verified" json:"email_verified"`
	LastLoginAt     *time.Time        `db:"last_login_at" json:"last_login_at,omitempty"`
	CreatedAt       time.Time         `db:"created_at" json:"created_at"`
	UpdatedAt       time.Time         `db:"updated_at" json:"updated_at"`
}

// ============================================================================
// Domain Methods
// ============================================================================

// IsActive verifica si el usuario está activo
func (u *User) IsActive() bool {
	return u.Status == UserStatusActive
}

// CanLogin verifica si el usuario puede iniciar sesión
func (u *User) CanLogin() bool {
	return u.IsActive() && u.EmailVerified
}

// Activate activa un usuario pendiente
func (u *User) Activate() error {
	if u.Status != UserStatusPending {
		return ErrInvalidStatus().WithDetail("current_status", u.Status)
	}

	u.Status = UserStatusActive
	u.UpdatedAt = time.Now()
	return nil
}

// Suspend suspende un usuario activo
func (u *User) Suspend(reason string) error {
	if !u.IsActive() {
		return ErrInvalidStatus().WithDetail("current_status", u.Status)
	}

	u.Status = UserStatusSuspended
	u.UpdatedAt = time.Now()
	return nil
}

// UpdateLastLogin actualiza la fecha del último login
func (u *User) UpdateLastLogin() {
	now := time.Now()
	u.LastLoginAt = &now
	u.UpdatedAt = now
}

// UpdateProfile actualiza la información del perfil
func (u *User) UpdateProfile(name, picture string) {
	if name != "" {
		u.Name = name
	}
	if picture != "" {
		u.Picture = ptrx.String(picture)
	}
	u.UpdatedAt = time.Now()
}

// ============================================================================
// Scope Management Methods
// ============================================================================

// HasScope verifica si el usuario tiene un scope específico
func (u *User) HasScope(scope string) bool {
	for _, s := range u.Scopes {
		// Exact match or wildcard "*"
		if s == scope || s == "*" {
			return true
		}
		// Wildcard match (e.g., "channels:*" matches "channels:read")
		if len(s) > 2 && s[len(s)-2:] == ":*" {
			prefix := s[:len(s)-2]
			if len(scope) > len(prefix) && scope[:len(prefix)] == prefix && scope[len(prefix)] == ':' {
				return true
			}
		}
	}
	return false
}

// IsAdmin verifica si el usuario tiene permisos de administrador
func (u *User) IsAdmin() bool {
	return u.HasScope("*") || u.HasScope("admin:*")
}

// HasAnyScope verifica si el usuario tiene alguno de los scopes proporcionados
func (u *User) HasAnyScope(scopes ...string) bool {
	return slices.ContainsFunc(scopes, u.HasScope)
}

// HasAllScopes verifica si el usuario tiene todos los scopes proporcionados
func (u *User) HasAllScopes(scopes ...string) bool {
	for _, scope := range scopes {
		if !u.HasScope(scope) {
			return false
		}
	}
	return true
}

// AddScope agrega un scope al usuario
func (u *User) AddScope(scope string) {
	if !u.HasScope(scope) {
		u.Scopes = append(u.Scopes, scope)
		u.UpdatedAt = time.Now()
	}
}

// RemoveScope remueve un scope del usuario
func (u *User) RemoveScope(scope string) {
	var newScopes []string
	for _, s := range u.Scopes {
		if s != scope {
			newScopes = append(newScopes, s)
		}
	}
	u.Scopes = newScopes
	u.UpdatedAt = time.Now()
}

// SetScopes establece los scopes del usuario
func (u *User) SetScopes(scopes []string) {
	u.Scopes = scopes
	u.UpdatedAt = time.Now()
}

// MakeAdmin convierte al usuario en administrador (asigna scope "*")
func (u *User) MakeAdmin() {
	u.AddScope("*")
}

// RevokeAdmin remueve permisos de administrador
func (u *User) RevokeAdmin() {
	u.RemoveScope("*")
	u.RemoveScope("admin:*")
}

// ============================================================================
// DTOs
// ============================================================================

// UserDetailsDTO contiene información básica de un usuario para otros módulos
type UserDetailsDTO struct {
	ID            kernel.UserID     `json:"id"`
	TenantID      kernel.TenantID   `json:"tenant_id"`
	Name          string            `json:"name"`
	Email         string            `json:"email"`
	Picture       *string           `json:"picture,omitempty"`
	IsActive      bool              `json:"is_active"`
	Scopes        []string          `json:"scopes"`
	OAuthProvider iam.OAuthProvider `json:"oauth_provider"`
}

// ToDTO convierte la entidad User a UserDetailsDTO
func (u *User) ToDTO() UserDetailsDTO {
	return UserDetailsDTO{
		ID:            u.ID,
		TenantID:      u.TenantID,
		Name:          u.Name,
		Email:         u.Email,
		Picture:       u.Picture,
		IsActive:      u.IsActive(),
		Scopes:        u.Scopes,
		OAuthProvider: u.OAuthProvider,
	}
}

// ============================================================================
// Service DTOs - Para operaciones de la capa de servicio
// ============================================================================

// CreateUserRequest representa la petición para crear un usuario
type CreateUserRequest struct {
	TenantID      kernel.TenantID `json:"tenant_id" validate:"required"`
	Email         string          `json:"email" validate:"required,email"`
	Name          string          `json:"name" validate:"required,min=2"`
	Scopes        []string        `json:"scopes,omitempty"`         // ✅ Direct scopes
	ScopeTemplate *string         `json:"scope_template,omitempty"` // ✅ Template name (e.g., "recruiter", "hiring_manager")
}

// UpdateUserRequest representa la petición para actualizar un usuario
type UpdateUserRequest struct {
	TenantID      kernel.TenantID `json:"tenant_id" validate:"required"`
	Name          *string         `json:"name,omitempty" validate:"omitempty,min=2"`
	Status        *UserStatus     `json:"status,omitempty"`
	Scopes        []string        `json:"scopes,omitempty"`         // ✅ Direct scopes to set
	ScopeTemplate *string         `json:"scope_template,omitempty"` // ✅ Template to apply
}

// InviteUserRequest para invitar usuarios a un tenant
type InviteUserRequest struct {
	Email         string   `json:"email" validate:"required,email"`
	Scopes        []string `json:"scopes,omitempty"`
	ScopeTemplate *string  `json:"scope_template,omitempty"`
}

// UserResponse representa la respuesta completa de un usuario
type UserResponse struct {
	User User `json:"user"`
}

// ToDTO convierte UserResponse a UserResponseDTO
func (ur *UserResponse) ToDTO() UserResponseDTO {
	return UserResponseDTO{
		User: ur.User.ToDTO(),
	}
}

// UserResponseDTO es la versión DTO de UserResponse
type UserResponseDTO struct {
	User UserDetailsDTO `json:"user"`
}

// SuspendUserRequest para suspender un usuario
type SuspendUserRequest struct {
	TenantID kernel.TenantID `json:"tenant_id" validate:"required"`
	Reason   string          `json:"reason" validate:"required,min=5"`
}

// ActivateUserRequest para activar un usuario
type ActivateUserRequest struct {
	TenantID kernel.TenantID `json:"tenant_id" validate:"required"`
}

// UserListResponse para listas de usuarios
type UserListResponse struct {
	Users []UserResponse `json:"users"`
	Total int            `json:"total"`
}

// ToDTO convierte UserListResponse a UserListResponseDTO
func (ulr *UserListResponse) ToDTO() UserListResponseDTO {
	var usersDTO []UserResponseDTO
	for _, u := range ulr.Users {
		usersDTO = append(usersDTO, u.ToDTO())
	}

	return UserListResponseDTO{
		Users: usersDTO,
		Total: ulr.Total,
	}
}

// UserListResponseDTO es la versión DTO de UserListResponse
type UserListResponseDTO struct {
	Users []UserResponseDTO `json:"users"`
	Total int               `json:"total"`
}

// ============================================================================
// Scope Management DTOs
// ============================================================================

// ScopeDetail información detallada de un scope
type ScopeDetail struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
}

// AddScopesRequest para agregar scopes a un usuario
type AddScopesRequest struct {
	TenantID kernel.TenantID `json:"tenant_id" validate:"required"`
	Scopes   []string        `json:"scopes" validate:"required,min=1"`
}

// RemoveScopesRequest para remover scopes de un usuario
type RemoveScopesRequest struct {
	TenantID kernel.TenantID `json:"tenant_id" validate:"required"`
	Scopes   []string        `json:"scopes" validate:"required,min=1"`
}

// SetScopesRequest para establecer scopes de un usuario
type SetScopesRequest struct {
	TenantID kernel.TenantID `json:"tenant_id" validate:"required"`
	Scopes   []string        `json:"scopes" validate:"required,min=1"`
}

// ApplyScopeTemplateRequest para aplicar una plantilla de scopes
type ApplyScopeTemplateRequest struct {
	TenantID     kernel.TenantID `json:"tenant_id" validate:"required"`
	TemplateName string          `json:"template_name" validate:"required"`
}

// UserScopesResponse respuesta con los scopes de un usuario
type UserScopesResponse struct {
	UserID       kernel.UserID `json:"user_id"`
	Scopes       []string      `json:"scopes"`
	ScopeDetails []ScopeDetail `json:"scope_details"`
	TotalScopes  int           `json:"total_scopes"`
	IsAdmin      bool          `json:"is_admin"`
}

// ScopeTemplateResponse respuesta con detalles de una plantilla
type ScopeTemplateResponse struct {
	TemplateName string        `json:"template_name"`
	Description  string        `json:"description,omitempty"`
	Scopes       []string      `json:"scopes"`
	ScopeDetails []ScopeDetail `json:"scope_details"`
	TotalScopes  int           `json:"total_scopes"`
}

// AvailableScopesResponse respuesta con todos los scopes disponibles
type AvailableScopesResponse struct {
	TotalScopes int                      `json:"total_scopes"`
	Categories  map[string][]ScopeDetail `json:"categories"`
	Templates   []string                 `json:"templates"`
}

// ============================================================================
// Error Registry - Errores específicos de User
// ============================================================================

var ErrRegistry = errx.NewRegistry("USER")

// Códigos de error
var (
	CodeUserNotFound         = ErrRegistry.Register("NOT_FOUND", errx.TypeNotFound, http.StatusNotFound, "Usuario no encontrado")
	CodeUserAlreadyExists    = ErrRegistry.Register("ALREADY_EXISTS", errx.TypeConflict, http.StatusConflict, "El usuario ya existe")
	CodeUserNotInTenant      = ErrRegistry.Register("NOT_IN_TENANT", errx.TypeAuthorization, http.StatusForbidden, "Usuario no pertenece a la empresa")
	CodeEmailNotVerified     = ErrRegistry.Register("EMAIL_NOT_VERIFIED", errx.TypeBusiness, http.StatusPreconditionFailed, "Email no verificado")
	CodeUserSuspended        = ErrRegistry.Register("SUSPENDED", errx.TypeBusiness, http.StatusForbidden, "Usuario suspendido")
	CodeOnboardingRequired   = ErrRegistry.Register("ONBOARDING_REQUIRED", errx.TypeBusiness, http.StatusPreconditionRequired, "Se requiere completar el onboarding")
	CodeInvalidStatus        = ErrRegistry.Register("INVALID_STATUS", errx.TypeBusiness, http.StatusBadRequest, "Estado de usuario inválido para esta operación")
	CodeInvalidScopeTemplate = ErrRegistry.Register("INVALID_SCOPE_TEMPLATE", errx.TypeValidation, http.StatusBadRequest, "Plantilla de scopes no encontrada")
	CodeInvalidScopes        = ErrRegistry.Register("INVALID_SCOPES", errx.TypeValidation, http.StatusBadRequest, "Scopes inválidos")
	CodeScopeNotFound        = ErrRegistry.Register("SCOPE_NOT_FOUND", errx.TypeNotFound, http.StatusNotFound, "Scope no encontrado")
	CodeInsufficientScopes   = ErrRegistry.Register("INSUFFICIENT_SCOPES", errx.TypeAuthorization, http.StatusForbidden, "Scopes insuficientes")
)

// Helper functions para crear errores
func ErrUserNotFound() *errx.Error {
	return ErrRegistry.New(CodeUserNotFound)
}

func ErrUserAlreadyExists() *errx.Error {
	return ErrRegistry.New(CodeUserAlreadyExists)
}

func ErrUserNotInTenant() *errx.Error {
	return ErrRegistry.New(CodeUserNotInTenant)
}

func ErrEmailNotVerified() *errx.Error {
	return ErrRegistry.New(CodeEmailNotVerified)
}

func ErrUserSuspended() *errx.Error {
	return ErrRegistry.New(CodeUserSuspended)
}

func ErrOnboardingRequired() *errx.Error {
	return ErrRegistry.New(CodeOnboardingRequired)
}

func ErrInvalidStatus() *errx.Error {
	return ErrRegistry.New(CodeInvalidStatus)
}

func ErrInvalidScopeTemplate() *errx.Error {
	return ErrRegistry.New(CodeInvalidScopeTemplate)
}

func ErrInvalidScopes() *errx.Error {
	return ErrRegistry.New(CodeInvalidScopes)
}

func ErrScopeNotFound() *errx.Error {
	return ErrRegistry.New(CodeScopeNotFound)
}

func ErrInsufficientScopes() *errx.Error {
	return ErrRegistry.New(CodeInsufficientScopes)
}
