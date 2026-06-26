package kernel

// ============================================================================
// Context Types
// ============================================================================

// AuthContext is the authentication context injected into each request
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

// IsValid checks whether the AuthContext is valid
func (ac *AuthContext) IsValid() bool {
	if ac.IsAPIKey {
		return !ac.TenantID.IsEmpty()
	}
	return ac.UserID != nil && !ac.UserID.IsEmpty() && !ac.TenantID.IsEmpty()
}

// ============================================================================
// Scope Management Methods
// ============================================================================

// MatchScope checks if a held scope grants access to the required scope.
// Supports exact match, global wildcard "*", and prefix wildcards (e.g., "roles:*" matches "roles:read").
func MatchScope(held, required string) bool {
	if held == required || held == "*" {
		return true
	}
	if len(held) > 2 && held[len(held)-2:] == ":*" {
		prefix := held[:len(held)-2]
		if len(required) > len(prefix) && required[:len(prefix)] == prefix && required[len(prefix)] == ':' {
			return true
		}
	}
	return false
}

// ScopesContain checks if a slice of scopes grants the required scope.
func ScopesContain(scopes []string, required string) bool {
	for _, s := range scopes {
		if MatchScope(s, required) {
			return true
		}
	}
	return false
}

// HasScope checks if the context has a specific scope
func (ac *AuthContext) HasScope(scope string) bool {
	return ScopesContain(ac.Scopes, scope)
}

// HasAnyScope checks whether the context has any of the provided scopes
func (ac *AuthContext) HasAnyScope(scopes ...string) bool {
	for _, scope := range scopes {
		if ac.HasScope(scope) {
			return true
		}
	}
	return false
}

// HasAllScopes checks whether the context has all of the provided scopes
func (ac *AuthContext) HasAllScopes(scopes ...string) bool {
	for _, scope := range scopes {
		if !ac.HasScope(scope) {
			return false
		}
	}
	return true
}

// ============================================================================
// Context Keys
// ============================================================================

type ContextKey string

const (
	// AuthContextKey is the key for storing AuthContext in context.Context
	AuthContextKey ContextKey = "auth_context"

	// TenantContextKey is the key for storing TenantID in context.Context
	TenantContextKey ContextKey = "tenant_id"

	// UserContextKey is the key for storing UserID in context.Context
	UserContextKey ContextKey = "user_id"

	// RequestIDKey is the key for storing the request ID
	RequestIDKey ContextKey = "request_id"
)
