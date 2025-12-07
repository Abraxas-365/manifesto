package tenant

import (
	"net/http"
	"time"

	"github.com/Abraxas-365/manifesto/pkg/errx"
	"github.com/Abraxas-365/manifesto/pkg/kernel"
)

// ============================================================================
// Tenant Entity
// ============================================================================

// TenantStatus define los posibles estados de un tenant
type TenantStatus string

const (
	TenantStatusActive    TenantStatus = "ACTIVE"
	TenantStatusSuspended TenantStatus = "SUSPENDED"
	TenantStatusCanceled  TenantStatus = "CANCELED"
	TenantStatusTrial     TenantStatus = "TRIAL"
)

// SubscriptionPlan define los planes de suscripción
type SubscriptionPlan string

const (
	PlanTrial        SubscriptionPlan = "TRIAL"
	PlanBasic        SubscriptionPlan = "BASIC"
	PlanProfessional SubscriptionPlan = "PROFESSIONAL"
	PlanEnterprise   SubscriptionPlan = "ENTERPRISE"
)

// Tenant es la entidad rica que representa una empresa en el sistema
type Tenant struct {
	ID                    kernel.TenantID  `db:"id" json:"id"`
	CompanyName           string           `db:"company_name" json:"company_name"`
	Status                TenantStatus     `db:"status" json:"status"`
	SubscriptionPlan      SubscriptionPlan `db:"subscription_plan" json:"subscription_plan"`
	MaxUsers              int              `db:"max_users" json:"max_users"`
	CurrentUsers          int              `db:"current_users" json:"current_users"`
	TrialExpiresAt        *time.Time       `db:"trial_expires_at" json:"trial_expires_at,omitempty"`
	SubscriptionExpiresAt *time.Time       `db:"subscription_expires_at" json:"subscription_expires_at,omitempty"`

	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

// ============================================================================
// Domain Methods
// ============================================================================

// IsActive verifica si el tenant está activo
func (t *Tenant) IsActive() bool {
	return t.Status == TenantStatusActive
}

// IsTrial verifica si el tenant está en período de prueba
func (t *Tenant) IsTrial() bool {
	return t.SubscriptionPlan == PlanTrial || t.Status == TenantStatusTrial
}

// IsTrialExpired verifica si el trial ha expirado
func (t *Tenant) IsTrialExpired() bool {
	if !t.IsTrial() || t.TrialExpiresAt == nil {
		return false
	}
	return time.Now().After(*t.TrialExpiresAt)
}

// IsSubscriptionExpired verifica si la suscripción ha expirado
func (t *Tenant) IsSubscriptionExpired() bool {
	if t.SubscriptionExpiresAt == nil {
		return false
	}
	return time.Now().After(*t.SubscriptionExpiresAt)
}

// CanAddUser verifica si se puede agregar un nuevo usuario
func (t *Tenant) CanAddUser() bool {
	if !t.IsActive() {
		return false
	}
	if t.IsTrialExpired() || t.IsSubscriptionExpired() {
		return false
	}
	return t.CurrentUsers < t.MaxUsers
}

// AddUser incrementa el contador de usuarios
func (t *Tenant) AddUser() error {
	if !t.CanAddUser() {
		return ErrMaxUsersReached().WithDetail("max_users", t.MaxUsers).WithDetail("current_users", t.CurrentUsers)
	}

	t.CurrentUsers++
	t.UpdatedAt = time.Now()
	return nil
}

// RemoveUser decrementa el contador de usuarios
func (t *Tenant) RemoveUser() {
	if t.CurrentUsers > 0 {
		t.CurrentUsers--
		t.UpdatedAt = time.Now()
	}
}

// Suspend suspende el tenant
func (t *Tenant) Suspend(reason string) {
	t.Status = TenantStatusSuspended
	t.UpdatedAt = time.Now()
}

// Activate activa el tenant
func (t *Tenant) Activate() {
	t.Status = TenantStatusActive
	t.UpdatedAt = time.Now()
}

// UpgradePlan mejora el plan de suscripción
func (t *Tenant) UpgradePlan(newPlan SubscriptionPlan) error {
	maxUsers := t.getMaxUsersForPlan(newPlan)
	if t.CurrentUsers > maxUsers {
		return ErrTooManyUsersForPlan().WithDetail("current_users", t.CurrentUsers).WithDetail("max_allowed", maxUsers)
	}

	t.SubscriptionPlan = newPlan
	t.MaxUsers = maxUsers
	t.UpdatedAt = time.Now()
	return nil
}

// getMaxUsersForPlan retorna el máximo de usuarios para un plan
func (t *Tenant) getMaxUsersForPlan(plan SubscriptionPlan) int {
	switch plan {
	case PlanTrial, PlanBasic:
		return 5
	case PlanProfessional:
		return 50
	case PlanEnterprise:
		return 500
	default:
		return 1
	}
}

// ============================================================================
// DTOs
// ============================================================================

// TenantDetailsDTO contiene información básica de un tenant para otros módulos
type TenantDetailsDTO struct {
	ID               kernel.TenantID  `json:"id"`
	CompanyName      string           `json:"company_name"`
	Status           TenantStatus     `json:"status"`
	SubscriptionPlan SubscriptionPlan `json:"subscription_plan"`
	MaxUsers         int              `json:"max_users"`
	CurrentUsers     int              `json:"current_users"`
}

// ToDTO convierte la entidad Tenant a TenantDetailsDTO
func (t *Tenant) ToDTO() TenantDetailsDTO {
	return TenantDetailsDTO{
		ID:               t.ID,
		CompanyName:      t.CompanyName,
		Status:           t.Status,
		SubscriptionPlan: t.SubscriptionPlan,
		MaxUsers:         t.MaxUsers,
		CurrentUsers:     t.CurrentUsers,
	}
}

// ============================================================================
// Service DTOs - Para operaciones de la capa de servicio
// ============================================================================

// CreateTenantRequest representa la petición para crear un tenant
type CreateTenantRequest struct {
	CompanyName      string           `json:"company_name" validate:"required,min=2"`
	SubscriptionPlan SubscriptionPlan `json:"subscription_plan"`
}

// UpdateTenantRequest representa la petición para actualizar un tenant
type UpdateTenantRequest struct {
	CompanyName *string       `json:"company_name,omitempty" validate:"omitempty,min=2"`
	Status      *TenantStatus `json:"status,omitempty"`
}

// TenantResponse representa la respuesta completa de un tenant con configuración
type TenantResponse struct {
	Tenant Tenant            `json:"tenant"`
	Config map[string]string `json:"config"`
}

// ToDTO convierte TenantResponse a TenantResponseDTO
func (tr *TenantResponse) ToDTO() TenantResponseDTO {
	return TenantResponseDTO{
		Tenant: tr.Tenant.ToDTO(),
		Config: tr.Config,
	}
}

// TenantResponseDTO es la versión DTO de TenantResponse
type TenantResponseDTO struct {
	Tenant TenantDetailsDTO  `json:"tenant"`
	Config map[string]string `json:"config"`
}

// SuspendTenantRequest para suspender un tenant
type SuspendTenantRequest struct {
	Reason string `json:"reason" validate:"required,min=10"`
}

// ActivateTenantRequest para activar un tenant
type ActivateTenantRequest struct {
	Comments string `json:"comments,omitempty"`
}

// UpgradePlanRequest para cambiar el plan de suscripción
type UpgradePlanRequest struct {
	NewPlan SubscriptionPlan `json:"new_plan" validate:"required"`
}

// SetConfigRequest para establecer una configuración
type SetConfigRequest struct {
	Key   string `json:"key" validate:"required"`
	Value string `json:"value" validate:"required"`
}

// DeleteConfigRequest para eliminar una configuración
type DeleteConfigRequest struct {
	Key string `json:"key" validate:"required"`
}

// TenantListResponse para listas de tenants
type TenantListResponse struct {
	Tenants []TenantResponse `json:"tenants"`
	Total   int              `json:"total"`
}

// ToDTO convierte TenantListResponse a TenantListResponseDTO
func (tlr *TenantListResponse) ToDTO() TenantListResponseDTO {
	var tenantsDTO []TenantResponseDTO
	for _, t := range tlr.Tenants {
		tenantsDTO = append(tenantsDTO, t.ToDTO())
	}

	return TenantListResponseDTO{
		Tenants: tenantsDTO,
		Total:   tlr.Total,
	}
}

// TenantListResponseDTO es la versión DTO de TenantListResponse
type TenantListResponseDTO struct {
	Tenants []TenantResponseDTO `json:"tenants"`
	Total   int                 `json:"total"`
}

// TenantStatsResponse para estadísticas del tenant
type TenantStatsResponse struct {
	TenantID              kernel.TenantID `json:"tenant_id"`
	TotalUsers            int             `json:"total_users"`
	ActiveUsers           int             `json:"active_users"`
	MaxUsers              int             `json:"max_users"`
	UserUtilization       float64         `json:"user_utilization"` // Porcentaje de usuarios usados
	SubscriptionStatus    string          `json:"subscription_status"`
	DaysUntilExpiration   *int            `json:"days_until_expiration,omitempty"`
	IsTrialExpired        bool            `json:"is_trial_expired"`
	IsSubscriptionExpired bool            `json:"is_subscription_expired"`
}

// TenantHealthResponse para el estado de salud del tenant
type TenantHealthResponse struct {
	TenantID        kernel.TenantID `json:"tenant_id"`
	Status          TenantStatus    `json:"status"`
	IsHealthy       bool            `json:"is_healthy"`
	Issues          []string        `json:"issues,omitempty"`
	LastHealthCheck time.Time       `json:"last_health_check"`
}

// BulkTenantOperationRequest para operaciones masivas
type BulkTenantOperationRequest struct {
	TenantIDs []kernel.TenantID `json:"tenant_ids" validate:"required,min=1"`
	Operation string            `json:"operation" validate:"required,oneof=suspend activate delete"`
	Reason    string            `json:"reason,omitempty"`
}

// BulkTenantOperationResponse resultado de operaciones masivas
type BulkTenantOperationResponse struct {
	Successful []kernel.TenantID          `json:"successful"`
	Failed     map[kernel.TenantID]string `json:"failed"`
	Total      int                        `json:"total"`
}

// TenantConfigResponse para respuestas de configuración
type TenantConfigResponse struct {
	TenantID kernel.TenantID   `json:"tenant_id"`
	Config   map[string]string `json:"config"`
}

// TenantUsageResponse para información de uso del tenant
type TenantUsageResponse struct {
	TenantID        kernel.TenantID `json:"tenant_id"`
	CurrentUsers    int             `json:"current_users"`
	MaxUsers        int             `json:"max_users"`
	UsagePercentage float64         `json:"usage_percentage"`
	CanAddUsers     bool            `json:"can_add_users"`
	RemainingUsers  int             `json:"remaining_users"`
}

// ============================================================================
// Error Registry - Errores específicos de Tenant
// ============================================================================

var ErrRegistry = errx.NewRegistry("TENANT")

// Códigos de error
var (
	CodeTenantNotFound      = ErrRegistry.Register("NOT_FOUND", errx.TypeNotFound, http.StatusNotFound, "Empresa no encontrada")
	CodeTenantAlreadyExists = ErrRegistry.Register("ALREADY_EXISTS", errx.TypeConflict, http.StatusConflict, "La empresa ya existe")
	CodeTenantSuspended     = ErrRegistry.Register("SUSPENDED", errx.TypeBusiness, http.StatusForbidden, "Empresa suspendida")
	CodeTrialExpired        = ErrRegistry.Register("TRIAL_EXPIRED", errx.TypeBusiness, http.StatusPaymentRequired, "Período de prueba expirado")
	CodeSubscriptionExpired = ErrRegistry.Register("SUBSCRIPTION_EXPIRED", errx.TypeBusiness, http.StatusPaymentRequired, "Suscripción expirada")
	CodeMaxUsersReached     = ErrRegistry.Register("MAX_USERS_REACHED", errx.TypeBusiness, http.StatusForbidden, "Máximo de usuarios alcanzado")
	CodeTooManyUsersForPlan = ErrRegistry.Register("TOO_MANY_USERS_FOR_PLAN", errx.TypeBusiness, http.StatusBadRequest, "El nuevo plan no permite tantos usuarios")
	CodeTenantHasUsers      = ErrRegistry.Register("TENANT_HAS_USERS", errx.TypeBusiness, http.StatusConflict, "No se puede eliminar tenant con usuarios activos")
	CodeInvalidPlanUpgrade  = ErrRegistry.Register("INVALID_PLAN_UPGRADE", errx.TypeBusiness, http.StatusBadRequest, "Actualización de plan inválida")
)

// Helper functions para crear errores
func ErrTenantNotFound() *errx.Error {
	return ErrRegistry.New(CodeTenantNotFound)
}

func ErrTenantAlreadyExists() *errx.Error {
	return ErrRegistry.New(CodeTenantAlreadyExists)
}

func ErrTenantSuspended() *errx.Error {
	return ErrRegistry.New(CodeTenantSuspended)
}

func ErrTrialExpired() *errx.Error {
	return ErrRegistry.New(CodeTrialExpired)
}

func ErrSubscriptionExpired() *errx.Error {
	return ErrRegistry.New(CodeSubscriptionExpired)
}

func ErrMaxUsersReached() *errx.Error {
	return ErrRegistry.New(CodeMaxUsersReached)
}

func ErrTooManyUsersForPlan() *errx.Error {
	return ErrRegistry.New(CodeTooManyUsersForPlan)
}

func ErrTenantHasUsers() *errx.Error {
	return ErrRegistry.New(CodeTenantHasUsers)
}

func ErrInvalidPlanUpgrade() *errx.Error {
	return ErrRegistry.New(CodeInvalidPlanUpgrade)
}
