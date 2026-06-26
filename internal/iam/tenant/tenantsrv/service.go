package tenantsrv

import (
	"context"
	"time"

	"github.com/Abraxas-365/manifesto/internal/config"
	"github.com/Abraxas-365/manifesto/internal/errx"
	"github.com/Abraxas-365/manifesto/internal/iam/tenant"
	"github.com/Abraxas-365/manifesto/internal/iam/user"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"github.com/google/uuid"
)

// TenantService provides business operations for tenants
type TenantService struct {
	tenantRepo       tenant.TenantRepository
	tenantConfigRepo tenant.TenantConfigRepository
	userRepo         user.UserRepository
	config           *config.TenantConfig
}

// NewTenantService creates a new instance of the tenant service
func NewTenantService(
	tenantRepo tenant.TenantRepository,
	tenantConfigRepo tenant.TenantConfigRepository,
	userRepo user.UserRepository,
	config *config.TenantConfig,
) *TenantService {
	return &TenantService{
		tenantRepo:       tenantRepo,
		tenantConfigRepo: tenantConfigRepo,
		userRepo:         userRepo,
		config:           config,
	}
}

// CreateTenant creates a new tenant
func (s *TenantService) CreateTenant(ctx context.Context, req tenant.CreateTenantRequest) (*tenant.Tenant, error) {
	// Create new tenant
	newTenant := &tenant.Tenant{
		ID:                    kernel.NewTenantID(uuid.NewString()),
		CompanyName:           req.CompanyName,
		Status:                tenant.TenantStatusTrial, // Starts in trial
		SubscriptionPlan:      tenant.PlanTrial,
		MaxUsers:              s.getMaxUsersForPlan(tenant.PlanTrial),
		CurrentUsers:          0,
		TrialExpiresAt:        s.calculateTrialExpiration(),
		SubscriptionExpiresAt: nil,
		CreatedAt:             time.Now().UTC(),
		UpdatedAt:             time.Now().UTC(),
	}

	// If a different plan was specified, use that
	if req.SubscriptionPlan != "" {
		newTenant.SubscriptionPlan = req.SubscriptionPlan
		newTenant.MaxUsers = s.getMaxUsersForPlan(req.SubscriptionPlan)
		if req.SubscriptionPlan != tenant.PlanTrial {
			newTenant.Status = tenant.TenantStatusActive
			newTenant.SubscriptionExpiresAt = s.calculateSubscriptionExpiration()
		}
	}

	// Save tenant
	if err := s.tenantRepo.Save(ctx, *newTenant); err != nil {
		return nil, errx.Wrap(err, "failed to save tenant", errx.TypeInternal)
	}

	return newTenant, nil
}

// GetTenantByID retrieves a tenant by ID
func (s *TenantService) GetTenantByID(ctx context.Context, tenantID kernel.TenantID) (*tenant.TenantResponse, error) {
	tenantEntity, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return nil, tenant.ErrTenantNotFound()
	}

	// Get tenant configurations
	config, err := s.tenantConfigRepo.FindByTenant(ctx, tenantID)
	if err != nil {
		config = make(map[string]string) // Default to empty config
	}

	return &tenant.TenantResponse{
		Tenant: *tenantEntity,
		Config: config,
	}, nil
}

// GetAllTenants retrieves all tenants
func (s *TenantService) GetAllTenants(ctx context.Context) (*tenant.TenantListResponse, error) {
	tenants, err := s.tenantRepo.FindAll(ctx)
	if err != nil {
		return nil, errx.Wrap(err, "failed to get all tenants", errx.TypeInternal)
	}

	var responses []tenant.TenantResponse
	for _, t := range tenants {
		config, _ := s.tenantConfigRepo.FindByTenant(ctx, t.ID)
		if config == nil {
			config = make(map[string]string)
		}
		responses = append(responses, tenant.TenantResponse{
			Tenant: *t,
			Config: config,
		})
	}

	return &tenant.TenantListResponse{
		Tenants: responses,
		Total:   len(responses),
	}, nil
}

// GetActiveTenants retrieves all active tenants
func (s *TenantService) GetActiveTenants(ctx context.Context) (*tenant.TenantListResponse, error) {
	tenants, err := s.tenantRepo.FindActive(ctx)
	if err != nil {
		return nil, errx.Wrap(err, "failed to get active tenants", errx.TypeInternal)
	}

	var responses []tenant.TenantResponse
	for _, t := range tenants {
		config, _ := s.tenantConfigRepo.FindByTenant(ctx, t.ID)
		if config == nil {
			config = make(map[string]string)
		}
		responses = append(responses, tenant.TenantResponse{
			Tenant: *t,
			Config: config,
		})
	}

	return &tenant.TenantListResponse{
		Tenants: responses,
		Total:   len(responses),
	}, nil
}

// UpdateTenant updates a tenant
func (s *TenantService) UpdateTenant(ctx context.Context, tenantID kernel.TenantID, req tenant.UpdateTenantRequest) (*tenant.Tenant, error) {
	tenantEntity, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return nil, tenant.ErrTenantNotFound()
	}

	// Update fields if provided
	if req.CompanyName != nil {
		tenantEntity.CompanyName = *req.CompanyName
	}
	if req.Status != nil {
		switch *req.Status {
		case tenant.TenantStatusActive:
			tenantEntity.Activate()
		case tenant.TenantStatusSuspended:
			tenantEntity.Suspend("Updated by admin")
		}
	}

	tenantEntity.UpdatedAt = time.Now().UTC()

	// Save changes
	if err := s.tenantRepo.Save(ctx, *tenantEntity); err != nil {
		return nil, errx.Wrap(err, "failed to update tenant", errx.TypeInternal)
	}

	return tenantEntity, nil
}

// SuspendTenant suspends a tenant
func (s *TenantService) SuspendTenant(ctx context.Context, tenantID kernel.TenantID, reason string) error {
	tenantEntity, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return tenant.ErrTenantNotFound()
	}

	tenantEntity.Suspend(reason)
	return s.tenantRepo.Save(ctx, *tenantEntity)
}

// ActivateTenant activates a tenant
func (s *TenantService) ActivateTenant(ctx context.Context, tenantID kernel.TenantID) error {
	tenantEntity, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return tenant.ErrTenantNotFound()
	}

	tenantEntity.Activate()
	return s.tenantRepo.Save(ctx, *tenantEntity)
}

// UpgradeTenantPlan upgrades the subscription plan of a tenant
func (s *TenantService) UpgradeTenantPlan(ctx context.Context, tenantID kernel.TenantID, newPlan tenant.SubscriptionPlan) error {
	tenantEntity, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return tenant.ErrTenantNotFound()
	}

	if err := tenantEntity.UpgradePlan(newPlan); err != nil {
		return err
	}

	// Update subscription expiration date
	if newPlan != tenant.PlanTrial {
		expirationDate := s.calculateSubscriptionExpiration()
		tenantEntity.SubscriptionExpiresAt = expirationDate
	}

	return s.tenantRepo.Save(ctx, *tenantEntity)
}

// GetTenantUsers retrieves all users for a tenant
func (s *TenantService) GetTenantUsers(ctx context.Context, tenantID kernel.TenantID) ([]*user.User, error) {
	// Verify that the tenant exists
	_, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return nil, tenant.ErrTenantNotFound()
	}

	users, err := s.userRepo.FindByTenant(ctx, tenantID)
	if err != nil {
		return nil, errx.Wrap(err, "failed to get tenant users", errx.TypeInternal)
	}

	return users, nil
}

// SetTenantConfig sets a tenant configuration
func (s *TenantService) SetTenantConfig(ctx context.Context, tenantID kernel.TenantID, key, value string) error {
	// Verify that the tenant exists
	_, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return tenant.ErrTenantNotFound()
	}

	return s.tenantConfigRepo.SaveSetting(ctx, tenantID, key, value)
}

// GetTenantConfig retrieves all tenant configurations
func (s *TenantService) GetTenantConfig(ctx context.Context, tenantID kernel.TenantID) (*tenant.TenantConfigResponse, error) {
	// Verify that the tenant exists
	_, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return nil, tenant.ErrTenantNotFound()
	}

	config, err := s.tenantConfigRepo.FindByTenant(ctx, tenantID)
	if err != nil {
		return nil, errx.Wrap(err, "failed to get tenant config", errx.TypeInternal)
	}

	return &tenant.TenantConfigResponse{
		TenantID: tenantID,
		Config:   config,
	}, nil
}

// DeleteTenantConfig deletes a tenant configuration
func (s *TenantService) DeleteTenantConfig(ctx context.Context, tenantID kernel.TenantID, key string) error {
	// Verify that the tenant exists
	_, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return tenant.ErrTenantNotFound()
	}

	return s.tenantConfigRepo.DeleteSetting(ctx, tenantID, key)
}

// GetTenantStats retrieves tenant statistics
func (s *TenantService) GetTenantStats(ctx context.Context, tenantID kernel.TenantID) (*tenant.TenantStatsResponse, error) {
	tenantEntity, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return nil, tenant.ErrTenantNotFound()
	}

	// Count active users
	users, err := s.userRepo.FindByTenant(ctx, tenantID)
	if err != nil {
		return nil, errx.Wrap(err, "failed to get users for stats", errx.TypeInternal)
	}

	activeUsers := 0
	for _, u := range users {
		if u.IsActive() {
			activeUsers++
		}
	}

	stats := &tenant.TenantStatsResponse{
		TenantID:              tenantEntity.ID,
		TotalUsers:            tenantEntity.CurrentUsers,
		ActiveUsers:           activeUsers,
		MaxUsers:              tenantEntity.MaxUsers,
		UserUtilization:       float64(tenantEntity.CurrentUsers) / float64(tenantEntity.MaxUsers) * 100,
		IsTrialExpired:        tenantEntity.IsTrialExpired(),
		IsSubscriptionExpired: tenantEntity.IsSubscriptionExpired(),
	}

	// Calculate days until expiration
	if tenantEntity.SubscriptionExpiresAt != nil && !tenantEntity.IsSubscriptionExpired() {
		days := int(time.Until(*tenantEntity.SubscriptionExpiresAt).Hours() / 24)
		stats.DaysUntilExpiration = &days
	}

	// Determine subscription status
	if tenantEntity.IsTrialExpired() {
		stats.SubscriptionStatus = "Trial Expired"
	} else if tenantEntity.IsSubscriptionExpired() {
		stats.SubscriptionStatus = "Subscription Expired"
	} else if tenantEntity.IsTrial() {
		stats.SubscriptionStatus = "Trial Active"
	} else {
		stats.SubscriptionStatus = "Subscription Active"
	}

	return stats, nil
}

// GetTenantUsage retrieves tenant usage information
func (s *TenantService) GetTenantUsage(ctx context.Context, tenantID kernel.TenantID) (*tenant.TenantUsageResponse, error) {
	tenantEntity, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return nil, tenant.ErrTenantNotFound()
	}

	usage := &tenant.TenantUsageResponse{
		TenantID:        tenantEntity.ID,
		CurrentUsers:    tenantEntity.CurrentUsers,
		MaxUsers:        tenantEntity.MaxUsers,
		UsagePercentage: float64(tenantEntity.CurrentUsers) / float64(tenantEntity.MaxUsers) * 100,
		CanAddUsers:     tenantEntity.CanAddUser(),
		RemainingUsers:  tenantEntity.MaxUsers - tenantEntity.CurrentUsers,
	}

	return usage, nil
}

// DeleteTenant deletes a tenant (soft delete recommended)
func (s *TenantService) DeleteTenant(ctx context.Context, tenantID kernel.TenantID) error {
	// Verify that the tenant exists
	tenantEntity, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return tenant.ErrTenantNotFound()
	}

	// Verify that there are no active users
	users, err := s.userRepo.FindByTenant(ctx, tenantID)
	if err == nil && len(users) > 0 {
		return tenant.ErrTenantHasUsers()
	}

	// Instead of deleting, permanently suspend
	tenantEntity.Suspend("Tenant deleted")
	return s.tenantRepo.Save(ctx, *tenantEntity)
}

// BulkSuspendTenants suspends multiple tenants
func (s *TenantService) BulkSuspendTenants(ctx context.Context, tenantIDs []kernel.TenantID, reason string) (*tenant.BulkTenantOperationResponse, error) {
	result := &tenant.BulkTenantOperationResponse{
		Successful: []kernel.TenantID{},
		Failed:     make(map[kernel.TenantID]string),
		Total:      len(tenantIDs),
	}

	for _, tenantID := range tenantIDs {
		if err := s.SuspendTenant(ctx, tenantID, reason); err != nil {
			result.Failed[tenantID] = err.Error()
		} else {
			result.Successful = append(result.Successful, tenantID)
		}
	}

	return result, nil
}

// BulkActivateTenants activates multiple tenants
func (s *TenantService) BulkActivateTenants(ctx context.Context, tenantIDs []kernel.TenantID) (*tenant.BulkTenantOperationResponse, error) {
	result := &tenant.BulkTenantOperationResponse{
		Successful: []kernel.TenantID{},
		Failed:     make(map[kernel.TenantID]string),
		Total:      len(tenantIDs),
	}

	for _, tenantID := range tenantIDs {
		if err := s.ActivateTenant(ctx, tenantID); err != nil {
			result.Failed[tenantID] = err.Error()
		} else {
			result.Successful = append(result.Successful, tenantID)
		}
	}

	return result, nil
}

// Helper methods
func (s *TenantService) getMaxUsersForPlan(plan tenant.SubscriptionPlan) int {
	switch plan {
	case tenant.PlanTrial, tenant.PlanBasic:
		return 5
	case tenant.PlanProfessional:
		return 50
	case tenant.PlanEnterprise:
		return 500
	default:
		return 1
	}
}

func (s *TenantService) calculateTrialExpiration() *time.Time {
	expiration := time.Now().UTC().AddDate(0, 0, s.config.TrialDays)
	return &expiration
}

func (s *TenantService) calculateSubscriptionExpiration() *time.Time {
	expiration := time.Now().UTC().AddDate(s.config.SubscriptionYears, 0, 0)
	return &expiration
}
