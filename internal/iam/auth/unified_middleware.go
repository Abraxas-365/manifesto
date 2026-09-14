package auth

import (
	"strings"

	"github.com/Abraxas-365/manifesto/internal/iam"
	"github.com/Abraxas-365/manifesto/internal/iam/apikey"
	"github.com/Abraxas-365/manifesto/internal/iam/apikey/apikeysrv"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"github.com/gofiber/fiber/v2"
)

type UnifiedAuthMiddleware struct {
	apiKeyService         *apikeysrv.APIKeyService
	tokenService          TokenService
	sessionRepo           SessionRepository
	accessTokenCookieName string
}

func NewAPIKeyMiddleware(
	apiKeyService *apikeysrv.APIKeyService,
	tokenService TokenService,
	sessionRepo SessionRepository,
	accessTokenCookieName string,
) *UnifiedAuthMiddleware {
	return &UnifiedAuthMiddleware{
		apiKeyService:         apiKeyService,
		tokenService:          tokenService,
		sessionRepo:           sessionRepo,
		accessTokenCookieName: accessTokenCookieName,
	}
}

func (am *UnifiedAuthMiddleware) Authenticate() fiber.Handler {
	return func(c *fiber.Ctx) error {
		apiKey := extractAPIKey(c)
		if apiKey != "" {
			return am.authenticateAPIKey(c, apiKey)
		}

		return am.authenticateJWT(c)
	}
}

func (am *UnifiedAuthMiddleware) authenticateAPIKey(c *fiber.Ctx, keyString string) error {
	key, err := am.apiKeyService.ValidateAPIKey(c.Context(), keyString)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	authContext := &kernel.AuthContext{
		UserID:   key.UserID,
		TenantID: key.TenantID,
		Scopes:   key.Scopes,
		IsAPIKey: true,
	}

	c.Locals("auth", authContext)
	c.Locals("api_key_id", key.ID)

	return c.Next()
}

// AuthenticateUserJWT authenticates user/session routes without granting scope
// authority. API keys cannot establish a user session. Existing locals are not
// accepted as a substitute for a valid JWT and, when present, an active session.
func (am *UnifiedAuthMiddleware) AuthenticateUserJWT() fiber.Handler {
	return func(c *fiber.Ctx) error {
		actor, err := am.readJWTIdentity(c)
		if err != nil {
			return err
		}
		c.Locals("auth", &kernel.AuthContext{
			UserID: actor.UserID, TenantID: actor.TenantID, SessionID: actor.SessionID,
		})
		return c.Next()
	}
}

func (am *UnifiedAuthMiddleware) authenticateJWT(c *fiber.Ctx) error {
	actor, err := am.readJWTIdentity(c)
	if err != nil {
		return err
	}
	c.Locals("auth", actor)
	return c.Next()
}

// readJWTIdentity shares extraction, validation and session checks. A nonempty
// Bearer credential takes precedence over the configured cookie, including when
// invalid. API-key headers/query parameters are not JWT credentials.
func (am *UnifiedAuthMiddleware) readJWTIdentity(c *fiber.Ctx) (*kernel.AuthContext, error) {
	var token string
	if parts := strings.SplitN(c.Get("Authorization"), " ", 2); len(parts) == 2 && parts[0] == "Bearer" && parts[1] != "" {
		token = parts[1]
	}
	if token == "" {
		token = c.Cookies(am.accessTokenCookieName)
	}
	if token == "" {
		return nil, iam.ErrUnauthorized()
	}
	claims, err := am.tokenService.ValidateAccessToken(token)
	if err != nil || claims == nil {
		return nil, iam.ErrUnauthorized()
	}
	actor := &kernel.AuthContext{
		UserID: &claims.UserID, TenantID: claims.TenantID, SessionID: claims.SessionID,
		Email: claims.Email, Name: claims.Name, Scopes: claims.Scopes,
	}
	if !actor.IsValid() {
		return nil, iam.ErrUnauthorized()
	}
	// Preserve support for tokens without session IDs. Session-bound tokens must
	// still resolve to an active session owned by the token's user and tenant.
	if claims.SessionID != "" {
		session, err := am.sessionRepo.FindSession(c.Context(), claims.SessionID)
		if err != nil || session == nil || session.IsExpired() || session.UserID != claims.UserID || session.TenantID != claims.TenantID {
			return nil, iam.ErrUnauthorized()
		}
		// Activity tracking remains best-effort and does not affect authentication.
		_ = am.sessionRepo.UpdateSessionActivity(c.Context(), claims.SessionID)
	}
	return actor, nil
}

// RequireScope - Requires a specific scope (works for both JWT and API keys)
func (am *UnifiedAuthMiddleware) RequireScope(scope string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		authContext, ok := GetAuthContext(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Authentication required",
			})
		}

		if !authContext.HasScope(scope) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error":          "Insufficient permissions",
				"required_scope": scope,
			})
		}

		return c.Next()
	}
}

// RequireAnyScope - Requires any of the provided scopes (works for both JWT and API keys)
func (am *UnifiedAuthMiddleware) RequireAnyScope(scopes ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		authContext, ok := GetAuthContext(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Authentication required",
			})
		}

		if !authContext.HasAnyScope(scopes...) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error":           "Insufficient permissions",
				"required_scopes": scopes,
			})
		}

		return c.Next()
	}
}

// RequireAllScopes - Requires ALL specified scopes (AND logic, works for both JWT and API keys)
func (am *UnifiedAuthMiddleware) RequireAllScopes(scopes ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		authContext, ok := GetAuthContext(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Authentication required",
			})
		}

		if !authContext.HasAllScopes(scopes...) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error":           "Insufficient permissions",
				"required_scopes": scopes,
			})
		}

		return c.Next()
	}
}

// Helper functions
func extractAPIKey(c *fiber.Ctx) string {
	authHeader := c.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && (parts[0] == "Bearer" || parts[0] == "X-API-Key") {
			if apikey.ValidateAPIKeyFormat(parts[1]) {
				return parts[1]
			}
		}
	}

	apiKeyHeader := c.Get("X-API-Key")
	if apiKeyHeader != "" && apikey.ValidateAPIKeyFormat(apiKeyHeader) {
		return apiKeyHeader
	}

	apiKeyQuery := c.Query("api_key")
	if apiKeyQuery != "" && apikey.ValidateAPIKeyFormat(apiKeyQuery) {
		return apiKeyQuery
	}

	return ""
}

// GetAuthContext helper to extract auth context from Fiber
func GetAuthContext(c *fiber.Ctx) (*kernel.AuthContext, bool) {
	authContext, ok := c.Locals("auth").(*kernel.AuthContext)
	return authContext, ok && authContext != nil && authContext.IsValid()
}
