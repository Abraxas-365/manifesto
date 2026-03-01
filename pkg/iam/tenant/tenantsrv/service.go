package tenantsrv

import (
	"context"
	"time"

	"github.com/Abraxas-365/manifesto/pkg/config"
	"github.com/Abraxas-365/manifesto/pkg/errx"
	"github.com/Abraxas-365/manifesto/pkg/iam/tenant"
	"github.com/Abraxas-365/manifesto/pkg/iam/user"
	"github.com/Abraxas-365/manifesto/pkg/kernel"
	"github.com/google/uuid"
)

// TenantService proporciona operaciones de negocio para tenants
type TenantService struct {
	tenantRepo       tenant.TenantRepository
	tenantConfigRepo tenant.TenantConfigRepository
	userRepo         user.UserRepository
	config           *config.TenantConfig
}

// NewTenantService crea una nueva instancia del servicio de tenants
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

// CreateTenant crea un nuevo tenant
func (s *TenantService) CreateTenant(ctx context.Context, req tenant.CreateTenantRequest) (*tenant.Tenant, error) {
	// Crear nuevo tenant
	newTenant := &tenant.Tenant{
		ID:                    kernel.NewTenantID(uuid.NewString()),
		CompanyName:           req.CompanyName,
		Status:                tenant.TenantStatusTrial, // Empieza en trial
		SubscriptionPlan:      tenant.PlanTrial,
		MaxUsers:              s.getMaxUsersForPlan(tenant.PlanTrial),
		CurrentUsers:          0,
		TrialExpiresAt:        s.calculateTrialExpiration(),
		SubscriptionExpiresAt: nil,
		CreatedAt:             time.Now().UTC(),
		UpdatedAt:             time.Now().UTC(),
	}

	// Si se especificó un plan diferente, usar ese
	if req.SubscriptionPlan != "" {
		newTenant.SubscriptionPlan = req.SubscriptionPlan
		newTenant.MaxUsers = s.getMaxUsersForPlan(req.SubscriptionPlan)
		if req.SubscriptionPlan != tenant.PlanTrial {
			newTenant.Status = tenant.TenantStatusActive
			newTenant.SubscriptionExpiresAt = s.calculateSubscriptionExpiration()
		}
	}

	// Guardar tenant
	if err := s.tenantRepo.Save(ctx, *newTenant); err != nil {
		return nil, errx.Wrap(err, "failed to save tenant", errx.TypeInternal)
	}

	return newTenant, nil
}

// GetTenantByID obtiene un tenant por ID
func (s *TenantService) GetTenantByID(ctx context.Context, tenantID kernel.TenantID) (*tenant.TenantResponse, error) {
	tenantEntity, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return nil, tenant.ErrTenantNotFound()
	}

	// Obtener configuraciones del tenant
	config, err := s.tenantConfigRepo.FindByTenant(ctx, tenantID)
	if err != nil {
		config = make(map[string]string) // Default a empty config
	}

	return &tenant.TenantResponse{
		Tenant: *tenantEntity,
		Config: config,
	}, nil
}

// GetAllTenants obtiene todos los tenants
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

// GetActiveTenants obtiene todos los tenants activos
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

// UpdateTenant actualiza un tenant
func (s *TenantService) UpdateTenant(ctx context.Context, tenantID kernel.TenantID, req tenant.UpdateTenantRequest) (*tenant.Tenant, error) {
	tenantEntity, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return nil, tenant.ErrTenantNotFound()
	}

	// Actualizar campos si se proporcionaron
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

	// Guardar cambios
	if err := s.tenantRepo.Save(ctx, *tenantEntity); err != nil {
		return nil, errx.Wrap(err, "failed to update tenant", errx.TypeInternal)
	}

	return tenantEntity, nil
}

// SuspendTenant suspende un tenant
func (s *TenantService) SuspendTenant(ctx context.Context, tenantID kernel.TenantID, reason string) error {
	tenantEntity, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return tenant.ErrTenantNotFound()
	}

	tenantEntity.Suspend(reason)
	return s.tenantRepo.Save(ctx, *tenantEntity)
}

// ActivateTenant activa un tenant
func (s *TenantService) ActivateTenant(ctx context.Context, tenantID kernel.TenantID) error {
	tenantEntity, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return tenant.ErrTenantNotFound()
	}

	tenantEntity.Activate()
	return s.tenantRepo.Save(ctx, *tenantEntity)
}

// UpgradeTenantPlan mejora el plan de suscripción de un tenant
func (s *TenantService) UpgradeTenantPlan(ctx context.Context, tenantID kernel.TenantID, newPlan tenant.SubscriptionPlan) error {
	tenantEntity, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return tenant.ErrTenantNotFound()
	}

	if err := tenantEntity.UpgradePlan(newPlan); err != nil {
		return err
	}

	// Actualizar fecha de expiración de suscripción
	if newPlan != tenant.PlanTrial {
		expirationDate := s.calculateSubscriptionExpiration()
		tenantEntity.SubscriptionExpiresAt = expirationDate
	}

	return s.tenantRepo.Save(ctx, *tenantEntity)
}

// GetTenantUsers obtiene todos los usuarios de un tenant
func (s *TenantService) GetTenantUsers(ctx context.Context, tenantID kernel.TenantID) ([]*user.User, error) {
	// Verificar que el tenant existe
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

// SetTenantConfig establece una configuración del tenant
func (s *TenantService) SetTenantConfig(ctx context.Context, tenantID kernel.TenantID, key, value string) error {
	// Verificar que el tenant existe
	_, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return tenant.ErrTenantNotFound()
	}

	return s.tenantConfigRepo.SaveSetting(ctx, tenantID, key, value)
}

// GetTenantConfig obtiene todas las configuraciones del tenant
func (s *TenantService) GetTenantConfig(ctx context.Context, tenantID kernel.TenantID) (*tenant.TenantConfigResponse, error) {
	// Verificar que el tenant existe
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

// DeleteTenantConfig elimina una configuración del tenant
func (s *TenantService) DeleteTenantConfig(ctx context.Context, tenantID kernel.TenantID, key string) error {
	// Verificar que el tenant existe
	_, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return tenant.ErrTenantNotFound()
	}

	return s.tenantConfigRepo.DeleteSetting(ctx, tenantID, key)
}

// GetTenantStats obtiene estadísticas del tenant
func (s *TenantService) GetTenantStats(ctx context.Context, tenantID kernel.TenantID) (*tenant.TenantStatsResponse, error) {
	tenantEntity, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return nil, tenant.ErrTenantNotFound()
	}

	// Contar usuarios activos
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

	// Calcular días hasta expiración
	if tenantEntity.SubscriptionExpiresAt != nil && !tenantEntity.IsSubscriptionExpired() {
		days := int(time.Until(*tenantEntity.SubscriptionExpiresAt).Hours() / 24)
		stats.DaysUntilExpiration = &days
	}

	// Determinar estado de suscripción
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

// GetTenantUsage obtiene información de uso del tenant
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

// DeleteTenant elimina un tenant (soft delete recomendado)
func (s *TenantService) DeleteTenant(ctx context.Context, tenantID kernel.TenantID) error {
	// Verificar que el tenant existe
	tenantEntity, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err != nil {
		return tenant.ErrTenantNotFound()
	}

	// Verificar que no tenga usuarios activos
	users, err := s.userRepo.FindByTenant(ctx, tenantID)
	if err == nil && len(users) > 0 {
		return tenant.ErrTenantHasUsers()
	}

	// En lugar de eliminar, suspender permanentemente
	tenantEntity.Suspend("Tenant deleted")
	return s.tenantRepo.Save(ctx, *tenantEntity)
}

// BulkSuspendTenants suspende múltiples tenants
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

// BulkActivateTenants activa múltiples tenants
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
