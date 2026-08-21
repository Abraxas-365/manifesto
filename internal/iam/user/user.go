package user

import (
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Abraxas-365/manifesto/internal/errx"
	"github.com/Abraxas-365/manifesto/internal/iam"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"github.com/Abraxas-365/manifesto/internal/ptrx"
	"slices"
)

// ============================================================================
// User Entity
// ============================================================================

// UserStatus defines possible states for a user
type UserStatus string

const (
	UserStatusActive    UserStatus = "ACTIVE"
	UserStatusInactive  UserStatus = "INACTIVE"
	UserStatusSuspended UserStatus = "SUSPENDED"
	UserStatusPending   UserStatus = "PENDING" // Invited but has not completed onboarding
)

// User entity representing a user in the system
type User struct {
	ID       kernel.UserID   `db:"id" json:"id"`
	TenantID kernel.TenantID `db:"tenant_id" json:"tenant_id"`
	Email    string          `db:"email" json:"email"`
	Name     string          `db:"name" json:"name"`
	Picture  *string         `db:"picture" json:"picture,omitempty"`

	// Authentication methods (can have multiple)
	OAuthProvider   iam.OAuthProvider `db:"oauth_provider" json:"oauth_provider"`
	OAuthProviderID string            `db:"oauth_provider_id" json:"oauth_provider_id"`
	OTPEnabled      bool              `db:"otp_enabled" json:"otp_enabled"` // NEW: Track if OTP is enabled

	Status        UserStatus `db:"status" json:"status"`
	Scopes        []string   `db:"scopes" json:"scopes"`
	EmailVerified bool       `db:"email_verified" json:"email_verified"`
	LastLoginAt   *time.Time `db:"last_login_at" json:"last_login_at,omitempty"`
	CreatedAt     time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at" json:"updated_at"`
}

// Domain methods
func (u *User) HasOAuth() bool {
	return u.OAuthProvider != "" && u.OAuthProviderID != ""
}

func (u *User) HasOTP() bool {
	return u.OTPEnabled
}

func (u *User) HasMultipleAuthMethods() bool {
	return u.HasOAuth() && u.HasOTP()
}

func (u *User) CanLoginWithOAuth() bool {
	return u.HasOAuth() && u.IsActive()
}

func (u *User) CanLoginWithOTP() bool {
	return u.HasOTP() && u.IsActive() && u.EmailVerified
}

func (u *User) EnableOTP() {
	u.OTPEnabled = true
	u.UpdatedAt = time.Now()
}

func (u *User) LinkOAuth(provider iam.OAuthProvider, providerID string) {
	u.OAuthProvider = provider
	u.OAuthProviderID = providerID
	u.UpdatedAt = time.Now()
}

// ============================================================================
// Domain Methods
// ============================================================================

// IsActive checks if the user is active
func (u *User) IsActive() bool {
	return u.Status == UserStatusActive
}

// CanLogin checks if the user can log in
func (u *User) CanLogin() bool {
	return u.IsActive() && u.EmailVerified
}

// Activate activates a pending user
func (u *User) Activate() error {
	if u.Status != UserStatusPending {
		return ErrInvalidStatus().WithDetail("current_status", u.Status)
	}

	u.Status = UserStatusActive
	u.UpdatedAt = time.Now()
	return nil
}

// Suspend suspends an active user
func (u *User) Suspend(reason string) error {
	if !u.IsActive() {
		return ErrInvalidStatus().WithDetail("current_status", u.Status)
	}

	u.Status = UserStatusSuspended
	u.UpdatedAt = time.Now()
	return nil
}

// UpdateLastLogin updates the last login timestamp
func (u *User) UpdateLastLogin() {
	now := time.Now()
	u.LastLoginAt = &now
	u.UpdatedAt = now
}

// UpdateProfile updates the user's profile information
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

// HasScope checks if the user has a specific scope
func (u *User) HasScope(scope string) bool {
	return kernel.ScopesContain(u.Scopes, scope)
}

// HasAnyScope checks if the user has any of the provided scopes
func (u *User) HasAnyScope(scopes ...string) bool {
	return slices.ContainsFunc(scopes, u.HasScope)
}

// HasAllScopes checks if the user has all of the provided scopes
func (u *User) HasAllScopes(scopes ...string) bool {
	for _, scope := range scopes {
		if !u.HasScope(scope) {
			return false
		}
	}
	return true
}

// AddScope adds a scope to the user
func (u *User) AddScope(scope string) {
	if !u.HasScope(scope) {
		u.Scopes = append(u.Scopes, scope)
		u.UpdatedAt = time.Now()
	}
}

// RemoveScope removes a scope from the user
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

// SetScopes sets the user's scopes
func (u *User) SetScopes(scopes []string) {
	u.Scopes = scopes
	u.UpdatedAt = time.Now()
}

// ============================================================================
// DTOs
// ============================================================================

// UserDetailsDTO contains basic user information for other modules
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

// ToDTO converts the User entity to UserDetailsDTO
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
// Service DTOs - For service layer operations
// ============================================================================

// CreateUserRequest represents the request to create a user
type CreateUserRequest struct {
	TenantID kernel.TenantID `json:"tenant_id"`
	Email    string          `json:"email"`
	Name     string          `json:"name"`
	Scopes   []string        `json:"scopes,omitempty"`
}

func (r *CreateUserRequest) Validate() error {
	if _, err := mail.ParseAddress(r.Email); err != nil {
		return errx.Validation("A valid email is required").WithDetail("field", "email")
	}
	if utf8.RuneCountInString(strings.TrimSpace(r.Name)) < 2 {
		return errx.Validation("Name must be at least 2 characters").WithDetail("field", "name")
	}
	return nil
}

// UpdateUserRequest represents the request to update a user
type UpdateUserRequest struct {
	TenantID kernel.TenantID `json:"tenant_id"`
	Name     *string         `json:"name,omitempty"`
	Status   *UserStatus     `json:"status,omitempty"`
	Scopes   []string        `json:"scopes,omitempty"`
}

func (r *UpdateUserRequest) Validate() error {
	if r.Name != nil && utf8.RuneCountInString(strings.TrimSpace(*r.Name)) < 2 {
		return errx.Validation("Name must be at least 2 characters").WithDetail("field", "name")
	}
	return nil
}

// UserResponse represents the full response for a user
type UserResponse struct {
	User User `json:"user"`
}

// ToDTO converts UserResponse to UserResponseDTO
func (ur *UserResponse) ToDTO() UserResponseDTO {
	return UserResponseDTO{
		User: ur.User.ToDTO(),
	}
}

// UserResponseDTO is the DTO version of UserResponse
type UserResponseDTO struct {
	User UserDetailsDTO `json:"user"`
}

// UserListResponse for lists of users
type UserListResponse struct {
	Users []UserResponse `json:"users"`
	Total int            `json:"total"`
}

// ToDTO converts UserListResponse to UserListResponseDTO
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

// UserListResponseDTO is the DTO version of UserListResponse
type UserListResponseDTO struct {
	Users []UserResponseDTO `json:"users"`
	Total int               `json:"total"`
}

// ============================================================================
// Scope Management DTOs
// ============================================================================

// ScopeDetail detailed information about a scope
type ScopeDetail struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
}

// AddScopesRequest for adding scopes to a user
type AddScopesRequest struct {
	Scopes []string `json:"scopes"`
}

func (r *AddScopesRequest) Validate() error {
	if len(r.Scopes) < 1 {
		return errx.Validation("At least one scope is required").WithDetail("field", "scopes")
	}
	return nil
}

// RemoveScopesRequest for removing scopes from a user
type RemoveScopesRequest struct {
	Scopes []string `json:"scopes"`
}

func (r *RemoveScopesRequest) Validate() error {
	if len(r.Scopes) < 1 {
		return errx.Validation("At least one scope is required").WithDetail("field", "scopes")
	}
	return nil
}

// SuspendUserRequest for suspending a user
type SuspendUserRequest struct {
	Reason string `json:"reason"`
}

func (r *SuspendUserRequest) Validate() error {
	if strings.TrimSpace(r.Reason) == "" {
		return errx.Validation("Reason is required").WithDetail("field", "reason")
	}
	return nil
}

// UserScopesResponse response with a user's scopes
type UserScopesResponse struct {
	UserID       kernel.UserID `json:"user_id"`
	Scopes       []string      `json:"scopes"`
	ScopeDetails []ScopeDetail `json:"scope_details"`
	TotalScopes  int           `json:"total_scopes"`
}

// AvailableScopesResponse response with all available scopes
type AvailableScopesResponse struct {
	TotalScopes int                      `json:"total_scopes"`
	Categories  map[string][]ScopeDetail `json:"categories"`
}

// ============================================================================
// Error Registry
// ============================================================================

var ErrRegistry = errx.NewRegistry("USER")

var (
	CodeUserNotFound       = ErrRegistry.Register("NOT_FOUND", errx.TypeNotFound, http.StatusNotFound, "User not found")
	CodeUserAlreadyExists  = ErrRegistry.Register("ALREADY_EXISTS", errx.TypeConflict, http.StatusConflict, "User already exists")
	CodeUserNotInTenant    = ErrRegistry.Register("NOT_IN_TENANT", errx.TypeAuthorization, http.StatusForbidden, "User does not belong to this tenant")
	CodeEmailNotVerified   = ErrRegistry.Register("EMAIL_NOT_VERIFIED", errx.TypeBusiness, http.StatusPreconditionFailed, "Email not verified")
	CodeUserSuspended      = ErrRegistry.Register("SUSPENDED", errx.TypeBusiness, http.StatusForbidden, "User suspended")
	CodeOnboardingRequired = ErrRegistry.Register("ONBOARDING_REQUIRED", errx.TypeBusiness, http.StatusPreconditionRequired, "Onboarding required")
	CodeInvalidStatus      = ErrRegistry.Register("INVALID_STATUS", errx.TypeBusiness, http.StatusBadRequest, "Invalid user status for this operation")
	CodeInvalidScopes      = ErrRegistry.Register("INVALID_SCOPES", errx.TypeValidation, http.StatusBadRequest, "Invalid scopes")
	CodeScopeNotFound      = ErrRegistry.Register("SCOPE_NOT_FOUND", errx.TypeNotFound, http.StatusNotFound, "Scope not found")
	CodeInsufficientScopes = ErrRegistry.Register("INSUFFICIENT_SCOPES", errx.TypeAuthorization, http.StatusForbidden, "Insufficient scopes")
)

// Helper functions
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

func ErrInvalidScopes() *errx.Error {
	return ErrRegistry.New(CodeInvalidScopes)
}

func ErrScopeNotFound() *errx.Error {
	return ErrRegistry.New(CodeScopeNotFound)
}

func ErrInsufficientScopes() *errx.Error {
	return ErrRegistry.New(CodeInsufficientScopes)
}
