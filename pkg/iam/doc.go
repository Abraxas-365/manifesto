// Package iam (Identity and Access Management) provides a complete authentication,
// authorization, and multi-tenant user management system for Go applications.
//
// # Overview
//
// The iam package is organized into several sub-packages that work together to
// provide a full IAM solution:
//
//   - iam/auth         — OAuth2, Passwordless (OTP), JWT tokens, sessions, middleware
//   - iam/user         — User entity, domain logic, scope management
//   - iam/tenant       — Tenant entity, subscription plans, lifecycle
//   - iam/invitation   — Invitation flow for onboarding users
//   - iam/apikey       — API key generation, validation, and management
//   - iam/otp          — One-time password generation and verification
//   - iam/scopes       — Scope definitions, groups, and validation
//
// # Architecture
//
// The package follows a layered, domain-driven architecture:
//
//	HTTP Handler  →  Service Layer  →  Repository Interface  →  Infrastructure (Postgres/Redis)
//
// Each sub-domain exposes its own error registry (e.g., "AUTH", "USER", "TENANT"),
// domain entities with rich methods, DTOs for API responses, and repository interfaces.
//
// # Authentication Methods
//
// Two authentication strategies are supported and can coexist on the same user:
//
//  1. OAuth2 — Sign in via Google or Microsoft. Users are created automatically
//     from invitation tokens on first login.
//
//  2. Passwordless (OTP) — Sign up and log in via a 6-digit code sent to the
//     user's email. Requires an invitation token for registration.
//
// Both methods produce the same JWT access/refresh token pair upon success.
//
// # Multi-Tenancy
//
// Every user belongs to a tenant (organization). A user's email can exist in
// multiple tenants independently. Tenant status, subscription plans, and user
// limits are enforced at the service layer.
//
// Subscription plans and their user limits:
//
//	TRIAL        → 5 users
//	BASIC        → 5 users
//	PROFESSIONAL → 50 users
//	ENTERPRISE   → 500 users
//
// # Scopes & Authorization
//
// Authorization is scope-based. Scopes follow the pattern "resource:action"
// (e.g., "users:read", "tenants:write"). The wildcard "*" grants full access.
// Scope groups (templates) make it easy to assign predefined sets of scopes:
//
//	super_admin, platform_admin, tenant_admin, user_manager,
//	analyst, api_admin, settings_admin, auditor, viewer
//
// # Middleware
//
// The UnifiedAuthMiddleware supports both JWT Bearer tokens and API keys
// transparently. It reads credentials from:
//
//   - Authorization: Bearer <token>
//   - Authorization: X-API-Key <key>
//   - X-API-Key: <key>
//   - ?api_key=<key>  (query param)
//   - access_token cookie
//
// # Quick Start
//
// Register all IAM routes on a Fiber router:
//
//	authHandlers.RegisterRoutes(app)           // OAuth2 + JWT
//	passwordlessHandlers.RegisterRoutes(app)   // OTP login/signup
//	invitationHandlers.RegisterRoutes(app, mw) // Invitation management
//	apiKeyHandlers.RegisterRoutes(app, mw)     // API key management
//
// Protect a route group:
//
//	api := app.Group("/api", middleware.Authenticate())
//	api.Get("/dashboard", myHandler)
//
// Require a specific scope:
//
//	app.Post("/users", middleware.Authenticate(), middleware.RequireScope("users:write"), createUser)
//
// Read the authenticated context inside a handler:
//
//	authCtx, ok := auth.GetAuthContext(c)
//	if !ok { ... }
//	fmt.Println(authCtx.UserID, authCtx.TenantID, authCtx.Scopes)
//
// # ──────────────────────────────────────────────────────
// # ENDPOINT REFERENCE
// # ──────────────────────────────────────────────────────
//
// ## OAuth2 Authentication  (registered by AuthHandlers)
//
// ### POST /auth/login
//
// Initiates an OAuth2 login flow. Returns an authorization URL to redirect the user to.
//
// Request body:
//
//	{
//	  "provider": "GOOGLE" | "MICROSOFT",
//	  "invitation_token": "<token>"   // required for first-time users
//	}
//
// Response 200:
//
//	{
//	  "auth_url": "https://accounts.google.com/o/oauth2/auth?...",
//	  "state":    "<random-state>"
//	}
//
// Notes:
//   - The provider value is case-insensitive ("google" == "GOOGLE").
//   - invitation_token is mandatory the first time a user signs up.
//     Subsequent logins without a token will look up the user by email.
//
// ### GET /auth/callback/:provider
//
// OAuth2 redirect callback. Called by the OAuth provider after the user grants
// consent. Do not call this endpoint directly.
//
// Path params:
//
//	provider — "google" | "microsoft"
//
// Query params:
//
//	code  — authorization code from provider
//	state — must match the state returned by /auth/login
//	error — set by provider on failure
//
// Response 200 (sets cookies access_token + refresh_token):
//
//	{
//	  "access_token":  "<jwt>",
//	  "refresh_token": "<jwt>",
//	  "token_type":    "Bearer",
//	  "expires_in":    900,
//	  "user": {
//	    "id": "...", "email": "...", "name": "...",
//	    "scopes": ["users:read", ...], "is_active": true, ...
//	  },
//	  "tenant": {
//	    "id": "...", "company_name": "...", "status": "ACTIVE",
//	    "subscription_plan": "PROFESSIONAL", ...
//	  }
//	}
//
// Error responses: 400 (invalid state / provider), 500 (token generation)
//
// ### POST /auth/refresh
//
// Refreshes an expired access token using a valid refresh token.
//
// Request body (or refresh_token cookie):
//
//	{ "refresh_token": "<jwt>" }
//
// Response 200 (updates access_token cookie):
//
//	{
//	  "access_token": "<new-jwt>",
//	  "token_type":   "Bearer",
//	  "expires_in":   900
//	}
//
// Error responses: 400 (missing token), 401 (invalid / expired refresh token)
//
// ### POST /auth/logout
//
// Revokes all refresh tokens and sessions for the authenticated user.
// Clears access_token and refresh_token cookies.
//
// Authentication: Bearer token or access_token cookie.
//
// Response 200:
//
//	{ "message": "Logged out successfully" }
//
// ### GET /auth/me
//
// Returns the full profile of the currently authenticated user and their tenant.
//
// Authentication: Bearer token or access_token cookie.
//
// Response 200:
//
//	{
//	  "user":   { ...UserDetailsDTO },
//	  "tenant": { ...TenantDetailsDTO }
//	}
//
// ## Passwordless (OTP) Authentication  (registered by PasswordlessAuthHandlers)
//
// ### POST /auth/passwordless/tenants
//
// Discovers which tenants an email address belongs to, and what authentication
// methods are available in each.
//
// Request body:
//
//	{ "email": "user@example.com" }
//
// Response 200:
//
//	{
//	  "email": "user@example.com",
//	  "tenants": [
//	    {
//	      "tenant_id": "...", "company_name": "Acme Corp",
//	      "user_status": "ACTIVE",
//	      "auth_methods": { "otp": true, "oauth": true, "oauth_provider": "GOOGLE" }
//	    }
//	  ],
//	  "count": 1
//	}
//
// Notes: Returns an empty list if the email is not found (does not reveal existence).
//
// ### POST /auth/passwordless/signup/initiate
//
// Creates a new user account and sends a verification OTP to the provided email.
// Requires a valid invitation token.
//
// Account linking: if the email already exists with OAuth only, OTP is enabled
// on the existing account instead of creating a new one.
//
// Request body:
//
//	{
//	  "email":            "user@example.com",
//	  "name":             "Jane Doe",
//	  "invitation_token": "<token>"
//	}
//
// Response 201 (new account):
//
//	{
//	  "message":      "Account created! Please check your email for verification code.",
//	  "email":        "user@example.com",
//	  "tenant_id":    "...",
//	  "requires_otp": true,
//	  "expires_in_seconds": 300
//	}
//
// Response 200 (account linking — OTP added to existing OAuth account):
//
//	{
//	  "message":       "OTP authentication linked to your existing account...",
//	  "account_linked": true,
//	  "can_login_with": { "otp": true, "oauth": true, "oauth_provider": "GOOGLE" }
//	}
//
// Error responses: 400 (invalid invitation), 403 (tenant inactive / user limit),
// 409 (OTP already enabled), 410 (invitation expired)
//
// ### POST /auth/passwordless/signup/verify
//
// Verifies the OTP code sent during signup, activates the account, and marks
// the email as verified. Does NOT return tokens — user must call login next.
//
// Request body:
//
//	{
//	  "email":     "user@example.com",
//	  "code":      "123456",
//	  "tenant_id": "..."
//	}
//
// Response 200:
//
//	{
//	  "message":   "Email verified! Your account is now active. You can log in.",
//	  "tenant_id": "...",
//	  "email":     "user@example.com"
//	}
//
// Error responses: 400 (invalid / expired code), 404 (user not found)
//
// ### POST /auth/passwordless/login/initiate
//
// Sends a login OTP to a verified, active user. The user must already have OTP
// enabled; otherwise, an OAuth suggestion is returned.
//
// Request body:
//
//	{
//	  "email":     "user@example.com",
//	  "tenant_id": "..."
//	}
//
// Response 200:
//
//	{
//	  "message":   "Login code sent to your email!",
//	  "email":     "user@example.com",
//	  "expires_in_seconds": 300,
//	  "auth_methods": { "otp": true, "oauth": false }
//	}
//
// Response 200 (email not found — does not reveal existence):
//
//	{ "message": "If this email is registered, you'll receive a login code.", "expires_in_seconds": 300 }
//
// Error responses: 400 (OTP not enabled, use OAuth), 403 (account inactive / unverified)
//
// ### POST /auth/passwordless/login/verify
//
// Verifies the login OTP and returns JWT access + refresh tokens.
//
// Request body:
//
//	{
//	  "email":     "user@example.com",
//	  "code":      "123456",
//	  "tenant_id": "..."
//	}
//
// Response 200 (sets cookies, identical shape to OAuth callback response):
//
//	{
//	  "access_token":  "<jwt>",
//	  "refresh_token": "<jwt>",
//	  "token_type":    "Bearer",
//	  "expires_in":    900,
//	  "user":   { ...UserDetailsDTO },
//	  "tenant": { ...TenantDetailsDTO }
//	}
//
// Error responses: 401 (invalid / expired code, user not found), 403 (inactive tenant)
//
// ### POST /auth/passwordless/resend-otp
//
// Resends a verification or login OTP. Rate-limited per the OTP configuration.
//
// Request body:
//
//	{
//	  "email":     "user@example.com",
//	  "tenant_id": "...",
//	  "purpose":   "signup" | "login"
//	}
//
// Response 200:
//
//	{ "message": "Verification code sent", "expires_in": 295 }
//
// Error responses: 400 (wrong purpose / account already verified),
// 403 (account inactive), 429 (rate limit exceeded)
//
// ## Invitations  (registered by InvitationHandlers — requires authentication)
//
// ### POST /invitations
//
// Creates and sends an invitation to a new user. The inviting user must have the
// "users:invite" scope or be an admin.
//
// Request body:
//
//	{
//	  "email":          "newuser@example.com",
//	  "scopes":         ["users:read", "reports:view"],  // direct scopes
//	  "scope_template": "viewer",                        // OR a template name
//	  "expires_in":     7                                // days, optional (default: configured)
//	}
//
// Response 201:
//
//	{
//	  "invitation": {
//	    "id": "...", "tenant_id": "...", "email": "...",
//	    "status": "PENDING", "scopes": [...],
//	    "expires_at": "2026-02-26T...", "created_at": "..."
//	  },
//	  "scope_templates": ["viewer", "tenant_admin", ...]
//	}
//
// Error responses: 400 (invalid scopes), 401, 403 (insufficient permissions),
// 404 (tenant not found), 409 (pending invitation already exists / user exists)
//
// ### GET /invitations
//
// Lists all invitations for the authenticated user's tenant.
//
// Response 200:
//
//	{ "invitations": [ ...InvitationResponseDTO ], "total": 5 }
//
// ### GET /invitations/pending
//
// Lists only PENDING (non-expired) invitations for the tenant.
//
// Response 200:
//
//	{ "invitations": [ ...InvitationResponseDTO ], "total": 2 }
//
// ### GET /invitations/:id
//
// Gets a single invitation by its UUID.
//
// Response 200: InvitationResponseDTO
// Error responses: 401, 404
//
// ### DELETE /invitations/:id
//
// Permanently deletes an invitation. Cannot delete already-accepted invitations.
//
// Response 200: { "message": "Invitation deleted successfully" }
// Error responses: 400 (already accepted), 401, 404
//
// ### POST /invitations/:id/revoke
//
// Revokes a pending invitation, preventing it from being used.
//
// Response 200: { "message": "Invitation revoked successfully" }
// Error responses: 400 (already accepted or revoked), 401, 404
//
// ### GET /invitations/public/token/:token
//
// Public endpoint. Retrieves invitation details by token string (used by the
// signup UI to pre-fill the form).
//
// Response 200: InvitationResponseDTO
// Error responses: 400 (missing token), 404
//
// ### GET /invitations/public/validate?token=<token>
//
// Public endpoint. Validates whether a token is still usable without consuming it.
//
// Response 200:
//
//	{
//	  "valid": true,
//	  "invitation": { ...InvitationDetailsDTO },
//	  "message": "Invitación válida"
//	}
//
//	// or when invalid:
//	{ "valid": false, "message": "Invitación expirada" }
//
// ## API Keys  (registered by APIKeyHandlers — requires authentication)
//
// API keys are an alternative to JWT for machine-to-machine authentication.
// They are passed via the Authorization header or X-API-Key header.
// The raw secret is shown exactly once upon creation and is never stored in plain text.
//
// ### POST /api-keys
//
// Creates a new API key for the authenticated tenant.
//
// Request body:
//
//	{
//	  "name":        "CI Pipeline Key",
//	  "description": "Used by GitHub Actions",
//	  "scopes":      ["reports:view", "users:read"],
//	  "environment": "live" | "test",
//	  "expires_in":  90,          // days, optional (omit for no expiration)
//	  "user_id":     "..."        // optional: associate with a specific user
//	}
//
// Response 201:
//
//	{
//	  "api_key": {
//	    "id": "...", "key_prefix": "manifesto_live_a1b2c3d4...",
//	    "tenant_id": "...", "name": "CI Pipeline Key",
//	    "scopes": [...], "is_active": true,
//	    "expires_at": "2026-05-19T...", "created_at": "..."
//	  },
//	  "secret_key": "manifesto_live_<64-char-hex>",
//	  "message": "⚠️ Save this key securely. It will not be shown again!"
//	}
//
// Error responses: 400 (validation), 401, 403 (tenant suspended)
//
// ### GET /api-keys
//
// Lists all API keys for the authenticated tenant (secrets are never returned).
//
// Response 200:
//
//	{ "api_keys": [ ...APIKeyDTO ], "total": 3 }
//
// ### GET /api-keys/:id
//
// Gets a single API key by its UUID.
//
// Response 200: APIKeyDTO
// Error responses: 401, 404
//
// ### PUT /api-keys/:id
//
// Updates mutable fields of an API key.
//
// Request body (all fields optional):
//
//	{
//	  "name":        "New Name",
//	  "description": "Updated description",
//	  "scopes":      ["users:read"],
//	  "is_active":   false
//	}
//
// Response 200: APIKeyDTO
// Error responses: 401, 404
//
// ### POST /api-keys/:id/revoke
//
// Immediately revokes the API key (sets is_active = false).
//
// Response 200: { "message": "API key revoked successfully" }
// Error responses: 401, 404
//
// ### DELETE /api-keys/:id
//
// Permanently deletes an API key record.
//
// Response 200: { "message": "API key deleted successfully" }
// Error responses: 401, 404
//
// # JWT Token Structure
//
// Access tokens (HS256) contain the following custom claims:
//
//	{
//	  "user_id":   "<UserID>",
//	  "tenant_id": "<TenantID>",
//	  "email":     "user@example.com",
//	  "name":      "Jane Doe",
//	  "scopes":    ["users:read", "reports:view"],
//	  "iss": "manifesto",
//	  "sub": "<UserID>",
//	  "iat": 1718000000,
//	  "exp": 1718000900
//	}
//
// Default TTLs:
//   - Access token:  15 minutes
//   - Refresh token: 7 days
//
// # Error Response Format
//
// All errors follow the errx structured format:
//
//	{
//	  "code":    "USER.NOT_FOUND",
//	  "message": "Usuario no encontrado",
//	  "type":    "NOT_FOUND",
//	  "details": { "user_id": "abc-123" }
//	}
//
// Common error codes by module:
//
//	IAM.UNAUTHORIZED            — 401  missing / invalid credentials
//	IAM.INVALID_TOKEN           — 401  JWT malformed or expired
//	IAM.ACCESS_DENIED           — 403  valid token, insufficient scope
//
//	AUTH.INVALID_REFRESH_TOKEN  — 401
//	AUTH.EXPIRED_REFRESH_TOKEN  — 401
//	AUTH.INVALID_OAUTH_PROVIDER — 400
//	AUTH.INVALID_STATE          — 400
//	AUTH.TOKEN_GENERATION_FAILED— 500
//
//	OTP.INVALID_OTP             — 400
//	OTP.OTP_EXPIRED             — 400
//	OTP.OTP_ALREADY_USED        — 400
//	OTP.TOO_MANY_ATTEMPTS       — 429
//	OTP.TOO_MANY_REQUESTS       — 429
//
//	USER.NOT_FOUND              — 404
//	USER.ALREADY_EXISTS         — 409
//	USER.SUSPENDED              — 403
//	USER.INVALID_SCOPES         — 400
//	USER.INVALID_SCOPE_TEMPLATE — 400
//
//	TENANT.NOT_FOUND            — 404
//	TENANT.SUSPENDED            — 403
//	TENANT.MAX_USERS_REACHED    — 403
//	TENANT.TRIAL_EXPIRED        — 402
//	TENANT.SUBSCRIPTION_EXPIRED — 402
//
//	INVITATION.NOT_FOUND        — 404
//	INVITATION.EXPIRED          — 410
//	INVITATION.ALREADY_EXISTS   — 409
//	INVITATION.USER_ALREADY_EXISTS — 409
//
//	APIKEY.NOT_FOUND            — 404
//	APIKEY.INVALID              — 401
//	APIKEY.EXPIRED              — 401
//	APIKEY.REVOKED              — 401
//
// # Infrastructure Dependencies
//
// Required:
//   - PostgreSQL — tenants, users, invitations, refresh_tokens, user_sessions,
//     password_reset_tokens, api_keys, otps, tenant_config
//
// Optional:
//   - Redis — RedisStateManager for OAuth state (replaces in-memory default)
//
// # State Management
//
// OAuth CSRF state tokens can be stored either in-memory (default, single-node)
// or in Redis (recommended for multi-node deployments):
//
//	// In-memory (default)
//	stateMgr := auth.NewInMemoryStateManager(10 * time.Minute)
//
//	// Redis (production)
//	stateMgr := authinfra.NewRedisStateManager(redisClient, 10*time.Minute)
//
// # Background Cleanup
//
// Use CleanupService to periodically remove expired tokens and sessions:
//
//	cleanup := authinfra.NewCleanupService(tokenRepo, sessionRepo, passwordResetRepo, 1*time.Hour)
//	go cleanup.Start(ctx)
package iam
