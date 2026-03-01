package invitation

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/Abraxas-365/manifesto/pkg/errx"
	"github.com/Abraxas-365/manifesto/pkg/kernel"
	"slices"
)

// ============================================================================
// Invitation Entity
// ============================================================================

// InvitationStatus define los posibles estados de una invitación
type InvitationStatus string

const (
	InvitationStatusPending  InvitationStatus = "PENDING"
	InvitationStatusAccepted InvitationStatus = "ACCEPTED"
	InvitationStatusExpired  InvitationStatus = "EXPIRED"
	InvitationStatusRevoked  InvitationStatus = "REVOKED"
)

// Invitation es la entidad que representa una invitación de usuario
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
// Getter Methods (para interfaces compatibles con auth)
// ============================================================================

// GetID retorna el ID de la invitación
func (i *Invitation) GetID() string {
	return i.ID
}

// GetTenantID retorna el TenantID de la invitación
func (i *Invitation) GetTenantID() kernel.TenantID {
	return i.TenantID
}

// GetEmail retorna el email de la invitación
func (i *Invitation) GetEmail() string {
	return i.Email
}

// GetScopes retorna los scopes de la invitación
func (i *Invitation) GetScopes() []string {
	return i.Scopes
}

// IsValid verifica si la invitación es válida
func (i *Invitation) IsValid() bool {
	return i.Status == InvitationStatusPending && time.Now().Before(i.ExpiresAt)
}

// IsExpired verifica si la invitación ha expirado
func (i *Invitation) IsExpired() bool {
	return time.Now().After(i.ExpiresAt)
}

// CanBeAccepted verifica si la invitación puede ser aceptada
func (i *Invitation) CanBeAccepted() bool {
	return i.Status == InvitationStatusPending && !i.IsExpired()
}

// Accept marca la invitación como aceptada
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

// Revoke revoca la invitación
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

// MarkAsExpired marca la invitación como expirada
func (i *Invitation) MarkAsExpired() {
	if i.Status == InvitationStatusPending && i.IsExpired() {
		i.Status = InvitationStatusExpired
		i.UpdatedAt = time.Now().UTC()
	}
}

// HasScope verifica si la invitación incluye un scope específico
func (i *Invitation) HasScope(scope string) bool {
	for _, s := range i.Scopes {
		if s == scope || s == "*" {
			return true
		}
	}
	return false
}

// HasAnyScope verifica si la invitación incluye alguno de los scopes
func (i *Invitation) HasAnyScope(scopes ...string) bool {
	return slices.ContainsFunc(scopes, i.HasScope)
}

// ============================================================================
// DTOs
// ============================================================================

// InvitationDetailsDTO contiene información básica de una invitación
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

// ToDTO convierte la entidad Invitation a InvitationDetailsDTO
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
// Service DTOs - Para operaciones de la capa de servicio
// ============================================================================

// CreateInvitationRequest representa la petición para crear una invitación
type CreateInvitationRequest struct {
	Email         string   `json:"email" validate:"required,email"`
	Scopes        []string `json:"scopes,omitempty"`         // ✅ Direct scopes
	ScopeTemplate *string  `json:"scope_template,omitempty"` // ✅ Optional: "recruiter", "hiring_manager", etc
	ExpiresIn     *int     `json:"expires_in,omitempty"`     // Días hasta expiración (default: 7)
}

// AcceptInvitationRequest representa la petición para aceptar una invitación
type AcceptInvitationRequest struct {
	Token string `json:"token" validate:"required"`
}

// InvitationResponse representa la respuesta con información de invitación
type InvitationResponse struct {
	Invitation     Invitation `json:"invitation"`
	ScopeTemplates []string   `json:"scope_templates,omitempty"` // ✅ Available templates
}

// ToDTO convierte InvitationResponse a InvitationResponseDTO
func (ir *InvitationResponse) ToDTO() InvitationResponseDTO {
	return InvitationResponseDTO{
		Invitation:     ir.Invitation.ToDTO(),
		ScopeTemplates: ir.ScopeTemplates,
	}
}

// InvitationResponseDTO es la versión DTO de InvitationResponse
type InvitationResponseDTO struct {
	Invitation     InvitationDetailsDTO `json:"invitation"`
	ScopeTemplates []string             `json:"scope_templates,omitempty"`
}

// InvitationListResponse para listas de invitaciones
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

// InvitationListResponseDTO es la versión DTO de InvitationListResponse
type InvitationListResponseDTO struct {
	Invitations []InvitationResponseDTO `json:"invitations"`
	Total       int                     `json:"total"`
}

// RevokeInvitationRequest para revocar una invitación
type RevokeInvitationRequest struct {
	Reason string `json:"reason,omitempty"`
}

// ValidateInvitationRequest para validar un token de invitación
type ValidateInvitationRequest struct {
	Token string `json:"token" validate:"required"`
}

// ValidateInvitationResponse respuesta de validación de invitación
type ValidateInvitationResponse struct {
	Valid      bool                  `json:"valid"`
	Invitation *InvitationDetailsDTO `json:"invitation,omitempty"`
	Message    string                `json:"message,omitempty"`
}

// ============================================================================
// Helper Functions
// ============================================================================

// GenerateInvitationToken genera un token único para la invitación
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
	CodeInvalidScopeTemplate      = ErrRegistry.Register("INVALID_SCOPE_TEMPLATE", errx.TypeValidation, http.StatusBadRequest, "Scope template not found")
	CodeInvalidScopes             = ErrRegistry.Register("INVALID_SCOPES", errx.TypeValidation, http.StatusBadRequest, "Invalid scopes")
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

func ErrInvalidScopeTemplate() *errx.Error {
	return ErrRegistry.New(CodeInvalidScopeTemplate)
}

func ErrInvalidScopes() *errx.Error {
	return ErrRegistry.New(CodeInvalidScopes)
}
