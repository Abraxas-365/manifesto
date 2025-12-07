package usersrv

import (
	"context"
	"time"

	"github.com/Abraxas-365/manifesto/pkg/errx"
	"github.com/Abraxas-365/manifesto/pkg/iam/auth"
	"github.com/Abraxas-365/manifesto/pkg/iam/tenant"
	"github.com/Abraxas-365/manifesto/pkg/iam/user"
	"github.com/Abraxas-365/manifesto/pkg/kernel"
	"github.com/google/uuid"
)

// UserService proporciona operaciones de negocio para usuarios
type UserService struct {
	userRepo    user.UserRepository
	tenantRepo  tenant.TenantRepository
	passwordSvc user.PasswordService
}

// NewUserService crea una nueva instancia del servicio de usuarios
func NewUserService(
	userRepo user.UserRepository,
	tenantRepo tenant.TenantRepository,
	passwordSvc user.PasswordService,
) *UserService {
	return &UserService{
		userRepo:    userRepo,
		tenantRepo:  tenantRepo,
		passwordSvc: passwordSvc,
	}
}

// CreateUser crea un nuevo usuario
func (s *UserService) CreateUser(ctx context.Context, req user.CreateUserRequest, creatorID kernel.UserID) (*user.User, error) {
	// Validar que el tenant exista y esté activo
	tenantEntity, err := s.tenantRepo.FindByID(ctx, req.TenantID)
	if err != nil {
		return nil, tenant.ErrTenantNotFound()
	}

	if !tenantEntity.IsActive() {
		return nil, tenant.ErrTenantSuspended()
	}

	// Verificar que el tenant puede agregar más usuarios
	if !tenantEntity.CanAddUser() {
		return nil, tenant.ErrMaxUsersReached()
	}

	// Verificar que no exista un usuario con el mismo email
	exists, err := s.userRepo.ExistsByEmail(ctx, req.Email, req.TenantID)
	if err != nil {
		return nil, errx.Wrap(err, "failed to check email existence", errx.TypeInternal)
	}
	if exists {
		return nil, user.ErrUserAlreadyExists()
	}

	// Determinar scopes
	scopes, err := s.resolveScopes(req)
	if err != nil {
		return nil, err
	}

	// Validar scopes
	if err := s.validateScopes(scopes); err != nil {
		return nil, err
	}

	// Crear nuevo usuario
	newUser := &user.User{
		ID:            kernel.NewUserID(uuid.NewString()),
		TenantID:      req.TenantID,
		Email:         req.Email,
		Name:          req.Name,
		Status:        user.UserStatusPending, // Pendiente hasta completar onboarding
		Scopes:        scopes,
		EmailVerified: false, // Se verificará después
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	// Guardar usuario
	if err := s.userRepo.Save(ctx, *newUser); err != nil {
		return nil, errx.Wrap(err, "failed to save user", errx.TypeInternal)
	}

	// Incrementar contador de usuarios del tenant
	if err := tenantEntity.AddUser(); err == nil {
		s.tenantRepo.Save(ctx, *tenantEntity)
	}

	return newUser, nil
}

// GetUserByID obtiene un usuario por ID
func (s *UserService) GetUserByID(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID) (*user.UserResponse, error) {
	userEntity, err := s.userRepo.FindByID(ctx, userID, tenantID)
	if err != nil {
		return nil, user.ErrUserNotFound()
	}

	return &user.UserResponse{
		User: *userEntity,
	}, nil
}

// GetUserByEmail obtiene un usuario por email
func (s *UserService) GetUserByEmail(ctx context.Context, email string, tenantID kernel.TenantID) (*user.UserResponse, error) {
	userEntity, err := s.userRepo.FindByEmail(ctx, email, tenantID)
	if err != nil {
		return nil, user.ErrUserNotFound()
	}

	return &user.UserResponse{
		User: *userEntity,
	}, nil
}

// GetUsersByTenant obtiene todos los usuarios de un tenant
func (s *UserService) GetUsersByTenant(ctx context.Context, tenantID kernel.TenantID) (*user.UserListResponse, error) {
	users, err := s.userRepo.FindByTenant(ctx, tenantID)
	if err != nil {
		return nil, errx.Wrap(err, "failed to get users by tenant", errx.TypeInternal)
	}

	var userResponses []user.UserResponse
	for _, u := range users {
		userResponses = append(userResponses, user.UserResponse{
			User: *u,
		})
	}

	return &user.UserListResponse{
		Users: userResponses,
		Total: len(userResponses),
	}, nil
}

// UpdateUser actualiza un usuario
func (s *UserService) UpdateUser(ctx context.Context, userID kernel.UserID, req user.UpdateUserRequest, updaterID kernel.UserID) (*user.User, error) {
	userEntity, err := s.userRepo.FindByID(ctx, userID, req.TenantID)
	if err != nil {
		return nil, user.ErrUserNotFound()
	}

	// Actualizar campos si se proporcionaron
	if req.Name != nil {
		userEntity.Name = *req.Name
	}

	if req.Status != nil {
		switch *req.Status {
		case user.UserStatusActive:
			if err := userEntity.Activate(); err != nil {
				return nil, err
			}
		case user.UserStatusSuspended:
			if err := userEntity.Suspend("Updated by admin"); err != nil {
				return nil, err
			}
		}
	}

	// Actualizar scopes si se proporcionaron
	if req.Scopes != nil && len(req.Scopes) > 0 {
		if err := s.validateScopes(req.Scopes); err != nil {
			return nil, err
		}
		userEntity.SetScopes(req.Scopes)
	}

	// Aplicar scope template si se proporciona
	if req.ScopeTemplate != nil && *req.ScopeTemplate != "" {
		scopes := auth.GetScopesByGroup(*req.ScopeTemplate)
		if len(scopes) == 0 {
			return nil, user.ErrInvalidScopeTemplate().
				WithDetail("template", *req.ScopeTemplate).
				WithDetail("available_templates", s.GetAvailableScopeTemplates())
		}
		userEntity.SetScopes(scopes)
	}

	userEntity.UpdatedAt = time.Now()

	// Guardar cambios
	if err := s.userRepo.Save(ctx, *userEntity); err != nil {
		return nil, errx.Wrap(err, "failed to update user", errx.TypeInternal)
	}

	return userEntity, nil
}

// ActivateUser activa un usuario pendiente
func (s *UserService) ActivateUser(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID) error {
	userEntity, err := s.userRepo.FindByID(ctx, userID, tenantID)
	if err != nil {
		return user.ErrUserNotFound()
	}

	if err := userEntity.Activate(); err != nil {
		return err
	}

	return s.userRepo.Save(ctx, *userEntity)
}

// SuspendUser suspende un usuario
func (s *UserService) SuspendUser(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID, reason string) error {
	userEntity, err := s.userRepo.FindByID(ctx, userID, tenantID)
	if err != nil {
		return user.ErrUserNotFound()
	}

	if err := userEntity.Suspend(reason); err != nil {
		return err
	}

	return s.userRepo.Save(ctx, *userEntity)
}

// DeleteUser elimina un usuario
func (s *UserService) DeleteUser(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID) error {
	// Verificar que el usuario existe
	_, err := s.userRepo.FindByID(ctx, userID, tenantID)
	if err != nil {
		return user.ErrUserNotFound()
	}

	// Eliminar usuario
	if err := s.userRepo.Delete(ctx, userID, tenantID); err != nil {
		return errx.Wrap(err, "failed to delete user", errx.TypeInternal)
	}

	// Decrementar contador de usuarios del tenant
	tenantEntity, err := s.tenantRepo.FindByID(ctx, tenantID)
	if err == nil {
		tenantEntity.RemoveUser()
		s.tenantRepo.Save(ctx, *tenantEntity)
	}

	return nil
}

// ============================================================================
// Scope Management Methods
// ============================================================================

// AddScopesToUser agrega scopes a un usuario
func (s *UserService) AddScopesToUser(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID, scopes []string) error {
	userEntity, err := s.userRepo.FindByID(ctx, userID, tenantID)
	if err != nil {
		return user.ErrUserNotFound()
	}

	// Validar scopes
	if err := s.validateScopes(scopes); err != nil {
		return err
	}

	// Agregar scopes (evitando duplicados)
	for _, scope := range scopes {
		if !userEntity.HasScope(scope) {
			userEntity.AddScope(scope)
		}
	}

	return s.userRepo.Save(ctx, *userEntity)
}

// RemoveScopesFromUser remueve scopes de un usuario
func (s *UserService) RemoveScopesFromUser(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID, scopes []string) error {
	userEntity, err := s.userRepo.FindByID(ctx, userID, tenantID)
	if err != nil {
		return user.ErrUserNotFound()
	}

	// Remover scopes
	for _, scope := range scopes {
		userEntity.RemoveScope(scope)
	}

	return s.userRepo.Save(ctx, *userEntity)
}

// SetUserScopes establece los scopes de un usuario (reemplaza los existentes)
func (s *UserService) SetUserScopes(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID, scopes []string) error {
	userEntity, err := s.userRepo.FindByID(ctx, userID, tenantID)
	if err != nil {
		return user.ErrUserNotFound()
	}

	// Validar scopes
	if err := s.validateScopes(scopes); err != nil {
		return err
	}

	userEntity.SetScopes(scopes)
	return s.userRepo.Save(ctx, *userEntity)
}

// ApplyScopeTemplateToUser aplica una plantilla de scopes a un usuario
func (s *UserService) ApplyScopeTemplateToUser(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID, templateName string) error {
	userEntity, err := s.userRepo.FindByID(ctx, userID, tenantID)
	if err != nil {
		return user.ErrUserNotFound()
	}

	scopes := auth.GetScopesByGroup(templateName)
	if len(scopes) == 0 {
		return user.ErrInvalidScopeTemplate().
			WithDetail("template", templateName).
			WithDetail("available_templates", s.GetAvailableScopeTemplates())
	}

	userEntity.SetScopes(scopes)
	return s.userRepo.Save(ctx, *userEntity)
}

// GetUserScopes obtiene los scopes de un usuario
func (s *UserService) GetUserScopes(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID) (*user.UserScopesResponse, error) {
	userEntity, err := s.userRepo.FindByID(ctx, userID, tenantID)
	if err != nil {
		return nil, user.ErrUserNotFound()
	}

	// Construir información detallada de scopes
	scopeDetails := make([]user.ScopeDetail, 0, len(userEntity.Scopes))
	for _, scope := range userEntity.Scopes {
		scopeDetails = append(scopeDetails, user.ScopeDetail{
			Name:        scope,
			Description: auth.GetScopeDescription(scope),
			Category:    auth.GetScopeCategory(scope),
		})
	}

	return &user.UserScopesResponse{
		UserID:       userID,
		Scopes:       userEntity.Scopes,
		ScopeDetails: scopeDetails,
		TotalScopes:  len(userEntity.Scopes),
		IsAdmin:      userEntity.IsAdmin(),
	}, nil
}

// MakeUserAdmin convierte a un usuario en administrador
func (s *UserService) MakeUserAdmin(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID) error {
	userEntity, err := s.userRepo.FindByID(ctx, userID, tenantID)
	if err != nil {
		return user.ErrUserNotFound()
	}

	userEntity.MakeAdmin()
	return s.userRepo.Save(ctx, *userEntity)
}

// RevokeUserAdmin revoca permisos de administrador
func (s *UserService) RevokeUserAdmin(ctx context.Context, userID kernel.UserID, tenantID kernel.TenantID) error {
	userEntity, err := s.userRepo.FindByID(ctx, userID, tenantID)
	if err != nil {
		return user.ErrUserNotFound()
	}

	userEntity.RevokeAdmin()
	return s.userRepo.Save(ctx, *userEntity)
}

// GetAvailableScopeTemplates retorna las plantillas de scopes disponibles
func (s *UserService) GetAvailableScopeTemplates() []string {
	templates := make([]string, 0, len(auth.ScopeGroups))
	for template := range auth.ScopeGroups {
		templates = append(templates, template)
	}
	return templates
}

// GetScopeTemplateDetails obtiene los detalles de una plantilla
func (s *UserService) GetScopeTemplateDetails(templateName string) (*user.ScopeTemplateResponse, error) {
	scopes := auth.GetScopesByGroup(templateName)
	if len(scopes) == 0 {
		return nil, user.ErrInvalidScopeTemplate().WithDetail("template", templateName)
	}

	scopeDetails := make([]user.ScopeDetail, 0, len(scopes))
	for _, scope := range scopes {
		scopeDetails = append(scopeDetails, user.ScopeDetail{
			Name:        scope,
			Description: auth.GetScopeDescription(scope),
			Category:    auth.GetScopeCategory(scope),
		})
	}

	return &user.ScopeTemplateResponse{
		TemplateName: templateName,
		Scopes:       scopes,
		ScopeDetails: scopeDetails,
		TotalScopes:  len(scopes),
	}, nil
}

// GetAllAvailableScopes retorna todos los scopes disponibles del sistema
func (s *UserService) GetAllAvailableScopes() *user.AvailableScopesResponse {
	allScopes := auth.GetAllScopes()
	categories := make(map[string][]user.ScopeDetail)

	// Agrupar por categoría
	for _, scope := range allScopes {
		category := auth.GetScopeCategory(scope)
		scopeDetail := user.ScopeDetail{
			Name:        scope,
			Description: auth.GetScopeDescription(scope),
			Category:    category,
		}
		categories[category] = append(categories[category], scopeDetail)
	}

	return &user.AvailableScopesResponse{
		TotalScopes: len(allScopes),
		Categories:  categories,
		Templates:   s.GetAvailableScopeTemplates(),
	}
}

// ============================================================================
// Private Helper Methods
// ============================================================================

// resolveScopes determina los scopes finales basándose en la request
func (s *UserService) resolveScopes(req user.CreateUserRequest) ([]string, error) {
	// Si se proporcionan scopes directamente, usarlos
	if len(req.Scopes) > 0 {
		return req.Scopes, nil
	}

	// Si se proporciona un template, expandirlo
	if req.ScopeTemplate != nil && *req.ScopeTemplate != "" {
		scopes := auth.GetScopesByGroup(*req.ScopeTemplate)
		if len(scopes) == 0 {
			return nil, user.ErrInvalidScopeTemplate().
				WithDetail("template", *req.ScopeTemplate).
				WithDetail("available_templates", s.GetAvailableScopeTemplates())
		}
		return scopes, nil
	}

	// Default: usar template "viewer" o scopes básicos
	defaultScopes := auth.GetScopesByGroup("viewer")
	if len(defaultScopes) == 0 {
		// Fallback a scopes muy básicos
		defaultScopes = []string{
			auth.ScopeUsersRead,
			auth.ScopeJobsRead,
			auth.ScopeCandidatesRead,
			auth.ScopeResumesRead,
		}
	}

	return defaultScopes, nil
}

// validateScopes valida que los scopes sean válidos
func (s *UserService) validateScopes(scopes []string) error {
	if len(scopes) == 0 {
		return user.ErrInvalidScopes().WithDetail("reason", "at least one scope is required")
	}

	// Validar cada scope
	invalidScopes := []string{}
	for _, scope := range scopes {
		if !auth.ValidateScope(scope) {
			invalidScopes = append(invalidScopes, scope)
		}
	}

	if len(invalidScopes) > 0 {
		return user.ErrInvalidScopes().
			WithDetail("invalid_scopes", invalidScopes).
			WithDetail("hint", "Use GetAllAvailableScopes() to see valid scopes")
	}

	return nil
}

