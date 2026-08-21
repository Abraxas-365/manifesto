package tenant

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Abraxas-365/manifesto/internal/errx"
	"github.com/Abraxas-365/manifesto/internal/kernel"
)

// ============================================================================
// Tenant Entity
// ============================================================================

// TenantStatus defines the possible statuses for a tenant
type TenantStatus string

const (
	TenantStatusActive    TenantStatus = "ACTIVE"
	TenantStatusSuspended TenantStatus = "SUSPENDED"
	TenantStatusCanceled  TenantStatus = "CANCELED"
	TenantStatusTrial     TenantStatus = "TRIAL"
)

// SubscriptionPlan defines the subscription plans
type SubscriptionPlan string

const (
	PlanTrial        SubscriptionPlan = "TRIAL"
	PlanBasic        SubscriptionPlan = "BASIC"
	PlanProfessional SubscriptionPlan = "PROFESSIONAL"
	PlanEnterprise   SubscriptionPlan = "ENTERPRISE"
)

// Tenant is the rich entity that represents a company in the system
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

// IsActive checks if the tenant is active
func (t *Tenant) IsActive() bool {
	return t.Status == TenantStatusActive
}

// IsTrial checks if the tenant is in a trial period
func (t *Tenant) IsTrial() bool {
	return t.SubscriptionPlan == PlanTrial || t.Status == TenantStatusTrial
}

// IsTrialExpired checks if the trial has expired
func (t *Tenant) IsTrialExpired() bool {
	if !t.IsTrial() || t.TrialExpiresAt == nil {
		return false
	}
	return time.Now().After(*t.TrialExpiresAt)
}

// IsSubscriptionExpired checks if the subscription has expired
func (t *Tenant) IsSubscriptionExpired() bool {
	if t.SubscriptionExpiresAt == nil {
		return false
	}
	return time.Now().After(*t.SubscriptionExpiresAt)
}

// CanAddUser checks if a new user can be added
func (t *Tenant) CanAddUser() bool {
	if !t.IsActive() {
		return false
	}
	if t.IsTrialExpired() || t.IsSubscriptionExpired() {
		return false
	}
	return t.CurrentUsers < t.MaxUsers
}

// AddUser increments the user counter
func (t *Tenant) AddUser() error {
	if !t.CanAddUser() {
		return ErrMaxUsersReached().WithDetail("max_users", t.MaxUsers).WithDetail("current_users", t.CurrentUsers)
	}

	t.CurrentUsers++
	t.UpdatedAt = time.Now()
	return nil
}

// RemoveUser decrements the user counter
func (t *Tenant) RemoveUser() {
	if t.CurrentUsers > 0 {
		t.CurrentUsers--
		t.UpdatedAt = time.Now()
	}
}

// Suspend suspends the tenant
func (t *Tenant) Suspend(reason string) {
	t.Status = TenantStatusSuspended
	t.UpdatedAt = time.Now()
}

// Activate activates the tenant
func (t *Tenant) Activate() {
	t.Status = TenantStatusActive
	t.UpdatedAt = time.Now()
}

// UpgradePlan upgrades the subscription plan
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

// getMaxUsersForPlan returns the maximum number of users for a plan
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

// TenantDetailsDTO contains basic tenant information for other modules
type TenantDetailsDTO struct {
	ID               kernel.TenantID  `json:"id"`
	CompanyName      string           `json:"company_name"`
	Status           TenantStatus     `json:"status"`
	SubscriptionPlan SubscriptionPlan `json:"subscription_plan"`
	MaxUsers         int              `json:"max_users"`
	CurrentUsers     int              `json:"current_users"`
}

// ToDTO converts the Tenant entity to TenantDetailsDTO
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
// Service DTOs - For service layer operations
// ============================================================================

// CreateTenantRequest represents the request to create a tenant
type CreateTenantRequest struct {
	CompanyName      string           `json:"company_name"`
	SubscriptionPlan SubscriptionPlan `json:"subscription_plan"`
}

// Validate validates the CreateTenantRequest
func (r *CreateTenantRequest) Validate() error {
	if utf8.RuneCountInString(strings.TrimSpace(r.CompanyName)) < 2 {
		return errx.Validation("Company name is required and must be at least 2 characters").WithDetail("field", "company_name")
	}
	return nil
}

// UpdateTenantRequest represents the request to update a tenant
type UpdateTenantRequest struct {
	CompanyName *string       `json:"company_name,omitempty"`
	Status      *TenantStatus `json:"status,omitempty"`
}

// Validate validates the UpdateTenantRequest
func (r *UpdateTenantRequest) Validate() error {
	if r.CompanyName != nil && utf8.RuneCountInString(strings.TrimSpace(*r.CompanyName)) < 2 {
		return errx.Validation("Company name must be at least 2 characters").WithDetail("field", "company_name")
	}
	return nil
}

// TenantResponse represents the complete tenant response with configuration
type TenantResponse struct {
	Tenant Tenant            `json:"tenant"`
	Config map[string]string `json:"config"`
}

// ToDTO converts TenantResponse to TenantResponseDTO
func (tr *TenantResponse) ToDTO() TenantResponseDTO {
	return TenantResponseDTO{
		Tenant: tr.Tenant.ToDTO(),
		Config: tr.Config,
	}
}

// TenantResponseDTO is the DTO version of TenantResponse
type TenantResponseDTO struct {
	Tenant TenantDetailsDTO  `json:"tenant"`
	Config map[string]string `json:"config"`
}

// SuspendTenantRequest for suspending a tenant
type SuspendTenantRequest struct {
	Reason string `json:"reason"`
}

// Validate validates the SuspendTenantRequest
func (r *SuspendTenantRequest) Validate() error {
	if utf8.RuneCountInString(strings.TrimSpace(r.Reason)) < 10 {
		return errx.Validation("Reason is required and must be at least 10 characters").WithDetail("field", "reason")
	}
	return nil
}

// ActivateTenantRequest for activating a tenant
type ActivateTenantRequest struct {
	Comments string `json:"comments,omitempty"`
}

// UpgradePlanRequest for changing the subscription plan
type UpgradePlanRequest struct {
	NewPlan SubscriptionPlan `json:"new_plan"`
}

// Validate validates the UpgradePlanRequest
func (r *UpgradePlanRequest) Validate() error {
	switch r.NewPlan {
	case PlanTrial, PlanBasic, PlanProfessional, PlanEnterprise:
		return nil
	default:
		return errx.Validation("New plan is required and must be a valid subscription plan").WithDetail("field", "new_plan")
	}
}

// SetConfigRequest for setting a configuration
type SetConfigRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Validate validates the SetConfigRequest
func (r *SetConfigRequest) Validate() error {
	if strings.TrimSpace(r.Key) == "" {
		return errx.Validation("Key is required").WithDetail("field", "key")
	}
	if strings.TrimSpace(r.Value) == "" {
		return errx.Validation("Value is required").WithDetail("field", "value")
	}
	return nil
}

// DeleteConfigRequest for deleting a configuration
type DeleteConfigRequest struct {
	Key string `json:"key"`
}

// Validate validates the DeleteConfigRequest
func (r *DeleteConfigRequest) Validate() error {
	if strings.TrimSpace(r.Key) == "" {
		return errx.Validation("Key is required").WithDetail("field", "key")
	}
	return nil
}

// TenantListResponse for tenant lists
type TenantListResponse struct {
	Tenants []TenantResponse `json:"tenants"`
	Total   int              `json:"total"`
}

// ToDTO converts TenantListResponse to TenantListResponseDTO
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

// TenantListResponseDTO is the DTO version of TenantListResponse
type TenantListResponseDTO struct {
	Tenants []TenantResponseDTO `json:"tenants"`
	Total   int                 `json:"total"`
}

// TenantStatsResponse for tenant statistics
type TenantStatsResponse struct {
	TenantID              kernel.TenantID `json:"tenant_id"`
	TotalUsers            int             `json:"total_users"`
	ActiveUsers           int             `json:"active_users"`
	MaxUsers              int             `json:"max_users"`
	UserUtilization       float64         `json:"user_utilization"` // Percentage of users used
	SubscriptionStatus    string          `json:"subscription_status"`
	DaysUntilExpiration   *int            `json:"days_until_expiration,omitempty"`
	IsTrialExpired        bool            `json:"is_trial_expired"`
	IsSubscriptionExpired bool            `json:"is_subscription_expired"`
}

// TenantHealthResponse for the tenant health status
type TenantHealthResponse struct {
	TenantID        kernel.TenantID `json:"tenant_id"`
	Status          TenantStatus    `json:"status"`
	IsHealthy       bool            `json:"is_healthy"`
	Issues          []string        `json:"issues,omitempty"`
	LastHealthCheck time.Time       `json:"last_health_check"`
}

// BulkTenantOperationRequest for bulk operations
type BulkTenantOperationRequest struct {
	TenantIDs []kernel.TenantID `json:"tenant_ids"`
	Operation string            `json:"operation"`
	Reason    string            `json:"reason,omitempty"`
}

// Validate validates the BulkTenantOperationRequest
func (r *BulkTenantOperationRequest) Validate() error {
	if len(r.TenantIDs) == 0 {
		return errx.Validation("At least one tenant ID is required").WithDetail("field", "tenant_ids")
	}
	switch r.Operation {
	case "suspend", "activate", "delete":
	default:
		return errx.Validation("Operation is required and must be one of: suspend, activate, delete").WithDetail("field", "operation")
	}
	return nil
}

// BulkTenantOperationResponse result of bulk operations
type BulkTenantOperationResponse struct {
	Successful []kernel.TenantID          `json:"successful"`
	Failed     map[kernel.TenantID]string `json:"failed"`
	Total      int                        `json:"total"`
}

// TenantConfigResponse for configuration responses
type TenantConfigResponse struct {
	TenantID kernel.TenantID   `json:"tenant_id"`
	Config   map[string]string `json:"config"`
}

// TenantUsageResponse for tenant usage information
type TenantUsageResponse struct {
	TenantID        kernel.TenantID `json:"tenant_id"`
	CurrentUsers    int             `json:"current_users"`
	MaxUsers        int             `json:"max_users"`
	UsagePercentage float64         `json:"usage_percentage"`
	CanAddUsers     bool            `json:"can_add_users"`
	RemainingUsers  int             `json:"remaining_users"`
}

// ============================================================================
// Error Registry
// ============================================================================

var ErrRegistry = errx.NewRegistry("TENANT")

var (
	CodeTenantNotFound      = ErrRegistry.Register("NOT_FOUND", errx.TypeNotFound, http.StatusNotFound, "Tenant not found")
	CodeTenantAlreadyExists = ErrRegistry.Register("ALREADY_EXISTS", errx.TypeConflict, http.StatusConflict, "Tenant already exists")
	CodeTenantSuspended     = ErrRegistry.Register("SUSPENDED", errx.TypeBusiness, http.StatusForbidden, "Tenant suspended")
	CodeTrialExpired        = ErrRegistry.Register("TRIAL_EXPIRED", errx.TypeBusiness, http.StatusPaymentRequired, "Trial period expired")
	CodeSubscriptionExpired = ErrRegistry.Register("SUBSCRIPTION_EXPIRED", errx.TypeBusiness, http.StatusPaymentRequired, "Subscription expired")
	CodeMaxUsersReached     = ErrRegistry.Register("MAX_USERS_REACHED", errx.TypeBusiness, http.StatusForbidden, "Maximum users reached")
	CodeTooManyUsersForPlan = ErrRegistry.Register("TOO_MANY_USERS_FOR_PLAN", errx.TypeBusiness, http.StatusBadRequest, "New plan does not support current user count")
	CodeTenantHasUsers      = ErrRegistry.Register("TENANT_HAS_USERS", errx.TypeBusiness, http.StatusConflict, "Cannot delete tenant with active users")
	CodeInvalidPlanUpgrade  = ErrRegistry.Register("INVALID_PLAN_UPGRADE", errx.TypeBusiness, http.StatusBadRequest, "Invalid plan upgrade")
)

// Helper functions
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
