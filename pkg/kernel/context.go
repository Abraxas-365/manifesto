package kernel

// ============================================================================
// Context Types - Tipos para context.Context
// ============================================================================

// AuthContext es el contexto de autenticación que se inyecta en cada request
type AuthContext struct {
	UserID   *UserID  `json:"user_id"`
	TenantID TenantID `json:"tenant_id"`
	Email    string   `json:"email"`
	Name     string   `json:"name"`
	Scopes   []string `json:"scopes"`
	IsAPIKey bool     `json:"is_api_key"`
}

// ============================================================================
// Validation Methods
// ============================================================================

// IsValid verifica si el AuthContext es válido
func (ac *AuthContext) IsValid() bool {
	if ac.IsAPIKey {
		return !ac.TenantID.IsEmpty()
	}
	return ac.UserID != nil && !ac.UserID.IsEmpty() && !ac.TenantID.IsEmpty()
}

// ============================================================================
// Scope Management Methods
// ============================================================================

// HasScope verifica si el contexto tiene un scope específico
func (ac *AuthContext) HasScope(scope string) bool {
	for _, s := range ac.Scopes {
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

// IsAdmin verifica si el contexto tiene permisos de administrador
func (ac *AuthContext) IsAdmin() bool {
	return ac.HasScope("*") || ac.HasScope("admin:*")
}

// HasAnyScope verifica si el contexto tiene alguno de los scopes proporcionados
func (ac *AuthContext) HasAnyScope(scopes ...string) bool {
	for _, scope := range scopes {
		if ac.HasScope(scope) {
			return true
		}
	}
	return false
}

// HasAllScopes verifica si el contexto tiene todos los scopes proporcionados
func (ac *AuthContext) HasAllScopes(scopes ...string) bool {
	for _, scope := range scopes {
		if !ac.HasScope(scope) {
			return false
		}
	}
	return true
}

// ============================================================================
// Context Keys - Claves para context.Context
// ============================================================================

type ContextKey string

const (
	// AuthContextKey es la clave para almacenar AuthContext en context.Context
	AuthContextKey ContextKey = "auth_context"

	// TenantContextKey es la clave para almacenar TenantID en context.Context
	TenantContextKey ContextKey = "tenant_id"

	// UserContextKey es la clave para almacenar UserID en context.Context
	UserContextKey ContextKey = "user_id"

	// RequestIDKey es la clave para almacenar el ID de la petición
	RequestIDKey ContextKey = "request_id"
)
