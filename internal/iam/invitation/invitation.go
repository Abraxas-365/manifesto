package invitation

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/Abraxas-365/manifesto/internal/errx"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"slices"
)

// ============================================================================
// Invitation Entity
// ============================================================================

// InvitationStatus defines the possible states of an invitation
type InvitationStatus string

const (
	InvitationStatusPending  InvitationStatus = "PENDING"
	InvitationStatusAccepted InvitationStatus = "ACCEPTED"
	InvitationStatusExpired  InvitationStatus = "EXPIRED"
	InvitationStatusRevoked  InvitationStatus = "REVOKED"
)

// Invitation is the entity that represents a user invitation
type Invitation struct {
	ID         string           `db:"id" json:"id"`
	TenantID   kernel.TenantID  `db:"tenant_id" json:"tenant_id"`
	Email      string           `db:"email" json:"email"`
	Token      string           `db:"token" json:"token"`
	Scopes     []string         `db:"scopes" json:"scopes"` // ✅ Changed from RoleID
	Status     InvitationStatus `db:"status" json:"status"`
	InvitedBy  kernel.UserID    `db:"invited_by" json:"invited_by"`
	ExpiresAt  time.Time        `db:"expires_at" json:"expires_at"`
	AcceptedAt *time.Time       `db:"accepted_at" json:"accepted_at,omitempty"`
	AcceptedBy *kernel.UserID   `db:"accepted_by" json:"accepted_by,omitempty"`
	CreatedAt  time.Time        `db:"created_at" json:"created_at"`
	UpdatedAt  time.Time        `db:"updated_at" json:"updated_at"`
}

// ============================================================================
// Domain Methods
// ============================================================================

// ============================================================================
// Getter Methods (for auth-compatible interfaces)
// ============================================================================

// GetID returns the invitation ID
func (i *Invitation) GetID() string {
	return i.ID
}

// GetTenantID returns the invitation TenantID
func (i *Invitation) GetTenantID() kernel.TenantID {
	return i.TenantID
}

// GetEmail returns the invitation email
func (i *Invitation) GetEmail() string {
	return i.Email
}

// GetScopes returns the invitation scopes
func (i *Invitation) GetScopes() []string {
	return i.Scopes
}

// IsValid checks whether the invitation is valid
func (i *Invitation) IsValid() bool {
	return i.Status == InvitationStatusPending && time.Now().Before(i.ExpiresAt)
}

// IsExpired checks whether the invitation has expired
func (i *Invitation) IsExpired() bool {
	return time.Now().After(i.ExpiresAt)
}

// CanBeAccepted checks whether the invitation can be accepted
func (i *Invitation) CanBeAccepted() bool {
	return i.Status == InvitationStatusPending && !i.IsExpired()
}

// Accept marks the invitation as accepted
func (i *Invitation) Accept(userID kernel.UserID) error {
	if !i.CanBeAccepted() {
		if i.IsExpired() {
			return ErrInvitationExpired()
		}
		return ErrInvitationInvalid().WithDetail("status", string(i.Status))
	}

	now := time.Now().UTC()
	i.Status = InvitationStatusAccepted
	i.AcceptedAt = &now
	i.AcceptedBy = &userID
	i.UpdatedAt = now

	return nil
}

// Revoke revokes the invitation
func (i *Invitation) Revoke() error {
	if i.Status == InvitationStatusAccepted {
		return ErrInvitationAlreadyAccepted()
	}
	if i.Status == InvitationStatusRevoked {
		return ErrInvitationAlreadyRevoked()
	}

	i.Status = InvitationStatusRevoked
	i.UpdatedAt = time.Now().UTC()
	return nil
}

// MarkAsExpired marks the invitation as expired
func (i *Invitation) MarkAsExpired() {
	if i.Status == InvitationStatusPending && i.IsExpired() {
		i.Status = InvitationStatusExpired
		i.UpdatedAt = time.Now().UTC()
	}
}

// HasScope checks whether the invitation includes a specific scope
func (i *Invitation) HasScope(scope string) bool {
	return kernel.ScopesContain(i.Scopes, scope)
}

// HasAnyScope checks whether the invitation includes any of the given scopes
func (i *Invitation) HasAnyScope(scopes ...string) bool {
	return slices.ContainsFunc(scopes, i.HasScope)
}

// ============================================================================
// DTOs
// ============================================================================

// InvitationDetailsDTO contains basic invitation information
type InvitationDetailsDTO struct {
	ID         string           `json:"id"`
	TenantID   kernel.TenantID  `json:"tenant_id"`
	Email      string           `json:"email"`
	Status     InvitationStatus `json:"status"`
	Scopes     []string         `json:"scopes"`
	ExpiresAt  time.Time        `json:"expires_at"`
	AcceptedAt *time.Time       `json:"accepted_at,omitempty"`
	CreatedAt  time.Time        `json:"created_at"`
}

// ToDTO converts the Invitation entity to InvitationDetailsDTO
func (i *Invitation) ToDTO() InvitationDetailsDTO {
	return InvitationDetailsDTO{
		ID:         i.ID,
		TenantID:   i.TenantID,
		Email:      i.Email,
		Status:     i.Status,
		Scopes:     i.Scopes,
		ExpiresAt:  i.ExpiresAt,
		AcceptedAt: i.AcceptedAt,
		CreatedAt:  i.CreatedAt,
	}
}

// ============================================================================
// Service DTOs - For service layer operations
// ============================================================================

// CreateInvitationRequest represents a request to create an invitation
type CreateInvitationRequest struct {
	Email     string   `json:"email" validate:"required,email"`
	Scopes    []string `json:"scopes,omitempty"`
	ExpiresIn *int     `json:"expires_in,omitempty"` // Days until expiration (default: 7)
}

// AcceptInvitationRequest represents a request to accept an invitation
type AcceptInvitationRequest struct {
	Token string `json:"token" validate:"required"`
}

// InvitationResponse represents a response with invitation information
type InvitationResponse struct {
	Invitation Invitation `json:"invitation"`
}

// ToDTO convierte InvitationResponse a InvitationResponseDTO
func (ir *InvitationResponse) ToDTO() InvitationResponseDTO {
	return InvitationResponseDTO{
		Invitation: ir.Invitation.ToDTO(),
	}
}

// InvitationResponseDTO is the DTO version of InvitationResponse
type InvitationResponseDTO struct {
	Invitation InvitationDetailsDTO `json:"invitation"`
}

// InvitationListResponse for invitation lists
type InvitationListResponse struct {
	Invitations []InvitationResponse `json:"invitations"`
	Total       int                  `json:"total"`
}

// ToDTO convierte InvitationListResponse a InvitationListResponseDTO
func (ilr *InvitationListResponse) ToDTO() InvitationListResponseDTO {
	var invitationsDTO []InvitationResponseDTO
	for _, inv := range ilr.Invitations {
		invitationsDTO = append(invitationsDTO, inv.ToDTO())
	}

	return InvitationListResponseDTO{
		Invitations: invitationsDTO,
		Total:       ilr.Total,
	}
}

// InvitationListResponseDTO is the DTO version of InvitationListResponse
type InvitationListResponseDTO struct {
	Invitations []InvitationResponseDTO `json:"invitations"`
	Total       int                     `json:"total"`
}

// RevokeInvitationRequest for revoking an invitation
type RevokeInvitationRequest struct {
	Reason string `json:"reason,omitempty"`
}

// ValidateInvitationRequest for validating an invitation token
type ValidateInvitationRequest struct {
	Token string `json:"token" validate:"required"`
}

// ValidateInvitationResponse is the invitation validation response
type ValidateInvitationResponse struct {
	Valid      bool                  `json:"valid"`
	Invitation *InvitationDetailsDTO `json:"invitation,omitempty"`
	Message    string                `json:"message,omitempty"`
}

// ============================================================================
// Helper Functions
// ============================================================================

// GenerateInvitationToken generates a unique token for the invitation
func GenerateInvitationToken(byteLength int) (string, error) {
	bytes := make([]byte, byteLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", errx.Wrap(err, "failed to generate invitation token", errx.TypeInternal)
	}
	return hex.EncodeToString(bytes), nil
}

func CalculateExpirationDate(daysFromNow int, defaultDays int) time.Time {
	if daysFromNow <= 0 {
		daysFromNow = defaultDays
	}
	return time.Now().UTC().AddDate(0, 0, daysFromNow)
}

// ============================================================================
// Error Registry
// ============================================================================

var ErrRegistry = errx.NewRegistry("INVITATION")

var (
	CodeInvitationNotFound        = ErrRegistry.Register("NOT_FOUND", errx.TypeNotFound, http.StatusNotFound, "Invitation not found")
	CodeInvitationExpired         = ErrRegistry.Register("EXPIRED", errx.TypeBusiness, http.StatusGone, "Invitation expired")
	CodeInvitationInvalid         = ErrRegistry.Register("INVALID", errx.TypeBusiness, http.StatusBadRequest, "Invalid invitation")
	CodeInvitationAlreadyAccepted = ErrRegistry.Register("ALREADY_ACCEPTED", errx.TypeBusiness, http.StatusConflict, "Invitation already accepted")
	CodeInvitationAlreadyRevoked  = ErrRegistry.Register("ALREADY_REVOKED", errx.TypeBusiness, http.StatusConflict, "Invitation already revoked")
	CodeInvitationAlreadyExists   = ErrRegistry.Register("ALREADY_EXISTS", errx.TypeConflict, http.StatusConflict, "A pending invitation already exists for this email")
	CodeUserAlreadyExists         = ErrRegistry.Register("USER_ALREADY_EXISTS", errx.TypeConflict, http.StatusConflict, "User already exists in this tenant")
	CodeInvalidScopes = ErrRegistry.Register("INVALID_SCOPES", errx.TypeValidation, http.StatusBadRequest, "Invalid scopes")
)

// Helper functions
func ErrInvitationNotFound() *errx.Error {
	return ErrRegistry.New(CodeInvitationNotFound)
}

func ErrInvitationExpired() *errx.Error {
	return ErrRegistry.New(CodeInvitationExpired)
}

func ErrInvitationInvalid() *errx.Error {
	return ErrRegistry.New(CodeInvitationInvalid)
}

func ErrInvitationAlreadyAccepted() *errx.Error {
	return ErrRegistry.New(CodeInvitationAlreadyAccepted)
}

func ErrInvitationAlreadyRevoked() *errx.Error {
	return ErrRegistry.New(CodeInvitationAlreadyRevoked)
}

func ErrInvitationAlreadyExists() *errx.Error {
	return ErrRegistry.New(CodeInvitationAlreadyExists)
}

func ErrUserAlreadyExists() *errx.Error {
	return ErrRegistry.New(CodeUserAlreadyExists)
}

func ErrInvalidScopes() *errx.Error {
	return ErrRegistry.New(CodeInvalidScopes)
}
