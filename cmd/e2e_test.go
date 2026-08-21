//go:build integration

// Package main e2e tests spin up real Postgres + Redis containers via
// testcontainers-go, run all migrations, boot the full application
// container/Fiber app in-process, and drive it purely over HTTP
// (fiber's app.Test, no real TCP listener).
//
// Run with: go test -tags=integration ./cmd/... -v -count=1
// Requires a working Docker daemon.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/Abraxas-365/manifesto/internal/config"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

// ============================================================================
// Test Infrastructure
// ============================================================================

// e2eEnv bundles the running app + containers for a single test run.
type e2eEnv struct {
	app       *fiber.App
	container *Container
}

// setupE2E boots Postgres + Redis testcontainers, applies every migration
// file in order, builds the real application container and Fiber app, and
// registers cleanup to tear everything down when the test finishes.
func setupE2E(t *testing.T) *e2eEnv {
	t.Helper()
	ctx := context.Background()

	migrationFiles, err := filepath.Glob("../migrations/*.up.sql")
	if err != nil || len(migrationFiles) == 0 {
		t.Fatalf("failed to find migration files: %v (found %d)", err, len(migrationFiles))
	}

	// Start Postgres
	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("manifesto_test"),
		tcpostgres.WithUsername("manifesto"),
		tcpostgres.WithPassword("testpassword"),
		tcpostgres.WithOrderedInitScripts(migrationFiles...),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(pgContainer); err != nil {
			t.Logf("failed to terminate postgres container: %v", err)
		}
	})

	pgHost, err := pgContainer.Host(ctx)
	if err != nil {
		t.Fatalf("failed to get postgres host: %v", err)
	}
	pgPort, err := pgContainer.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("failed to get postgres port: %v", err)
	}

	// Start Redis
	redisContainer, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Fatalf("failed to start redis container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(redisContainer); err != nil {
			t.Logf("failed to terminate redis container: %v", err)
		}
	})

	redisHost, err := redisContainer.Host(ctx)
	if err != nil {
		t.Fatalf("failed to get redis host: %v", err)
	}
	redisPort, err := redisContainer.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatalf("failed to get redis port: %v", err)
	}

	// Set env vars for config
	t.Setenv("DB_HOST", pgHost)
	t.Setenv("DB_PORT", pgPort.Port())
	t.Setenv("DB_USER", "manifesto")
	t.Setenv("DB_PASSWORD", "testpassword")
	t.Setenv("DB_NAME", "manifesto_test")
	t.Setenv("DB_SSL_MODE", "disable")
	t.Setenv("REDIS_HOST", redisHost)
	t.Setenv("REDIS_PORT", redisPort.Port())
	t.Setenv("REDIS_PASSWORD", "")
	t.Setenv("JWT_SECRET_KEY", "e2e-test-secret-key-at-least-32-chars")
	t.Setenv("JWT_ACCESS_TOKEN_TTL", "15m")
	t.Setenv("JWT_REFRESH_TOKEN_TTL", "168h")
	t.Setenv("UPLOAD_DIR", t.TempDir())
	t.Setenv("STORAGE_MODE", "local")
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("LOG_LEVEL", "error")
	t.Setenv("SERVER_PORT", "0")
	t.Setenv("BCRYPT_COST", "4") // fast for tests
	t.Setenv("OTP_RATE_LIMIT_WINDOW", "0s")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	container := NewContainer(cfg)
	t.Cleanup(container.Cleanup)

	// Build Fiber app (same as server.go but without startup/shutdown)
	app := fiber.New(fiber.Config{
		AppName:               "Manifesto E2E Test",
		DisableStartupMessage: true,
		ErrorHandler:          globalErrorHandler(cfg),
	})
	app.Use(recover.New())
	app.Get("/health", healthCheckHandler(container))
	registerRoutes(app, container)

	return &e2eEnv{app: app, container: container}
}

// do performs an HTTP request against the in-process app and decodes
// a JSON response body into out (if non-nil).
func (e *e2eEnv) do(t *testing.T, method, path, token string, body any, out any) *http.Response {
	t.Helper()

	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal request body: %v", err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, path, reader)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := e.app.Test(req, -1)
	if err != nil {
		t.Fatalf("request %s %s failed: %v", method, path, err)
	}

	if out != nil {
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("failed to read response body: %v", err)
		}
		if len(data) > 0 {
			if err := json.Unmarshal(data, out); err != nil {
				t.Fatalf("failed to decode response body %q: %v", string(data), err)
			}
		}
		// Replace body so callers can still check status
		resp.Body = io.NopCloser(bytes.NewReader(data))
	}

	return resp
}

// assertStatus is a helper to check HTTP status codes with a clear message.
func assertStatus(t *testing.T, resp *http.Response, expected int) {
	t.Helper()
	if resp.StatusCode != expected {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status %d, got %d; body: %s", expected, resp.StatusCode, string(body))
	}
}

// testUser holds an authenticated identity for use across tests.
type testUser struct {
	token        string
	refreshToken string
	userID       string
	tenantID     string
	email        string
}

// seedAdmin inserts a tenant + superadmin user directly into the database
// and generates a JWT token for them. This bypasses the invitation/signup
// flow and gives us a fully privileged user to test all endpoints.
func seedAdmin(t *testing.T, e *e2eEnv, companyName string) testUser {
	t.Helper()

	tenantID := uuid.NewString()
	userID := uuid.NewString()
	email := fmt.Sprintf("admin-%s@e2e-test.example.com", tenantID[:8])

	// Insert tenant
	_, err := e.container.DB.Exec(`
		INSERT INTO tenants (id, company_name, status, subscription_plan, max_users, current_users, created_at, updated_at)
		VALUES ($1, $2, 'ACTIVE', 'ENTERPRISE', 500, 1, NOW(), NOW())`,
		tenantID, companyName)
	if err != nil {
		t.Fatalf("failed to seed tenant: %v", err)
	}

	// Insert superadmin user with wildcard scope
	_, err = e.container.DB.Exec(`
		INSERT INTO users (id, tenant_id, email, name, picture, status, scopes, oauth_provider, oauth_provider_id, email_verified, otp_enabled, created_at, updated_at)
		VALUES ($1, $2, $3, 'E2E Admin', '', 'ACTIVE', $4, '', '', TRUE, TRUE, NOW(), NOW())`,
		userID, tenantID, email, pq.Array([]string{"*"}))
	if err != nil {
		t.Fatalf("failed to seed admin user: %v", err)
	}

	// Generate JWT
	token, err := e.container.IAM.TokenService.GenerateAccessToken(
		kernel.UserID(userID),
		kernel.TenantID(tenantID),
		map[string]any{
			"email":  email,
			"name":   "E2E Admin",
			"scopes": []string{"*"},
		},
	)
	if err != nil {
		t.Fatalf("failed to generate admin JWT: %v", err)
	}

	return testUser{
		token:    token,
		userID:   userID,
		tenantID: tenantID,
		email:    email,
	}
}

// ============================================================================
// Auth Flow Tests
// ============================================================================

func TestPasswordlessSignupLoginFlow(t *testing.T) {
	e := setupE2E(t)
	admin := seedAdmin(t, e, "Auth Flow Co")

	// 1. Create an invitation for the new user
	inviteeEmail := fmt.Sprintf("invitee-%s@e2e-test.example.com", uuid.NewString()[:8])

	var invitation struct {
		ID       string   `json:"id"`
		Email    string   `json:"email"`
		Scopes   []string `json:"scopes"`
		TenantID string   `json:"tenant_id"`
	}
	resp := e.do(t, http.MethodPost, "/api/v1/invitations", admin.token, fiber.Map{
		"email":  inviteeEmail,
		"scopes": []string{"users:read", "roles:read"},
	}, &invitation)
	assertStatus(t, resp, http.StatusCreated)

	// Token is not exposed in API response (security), fetch from DB
	var invitationToken string
	err := e.container.DB.Get(&invitationToken,
		`SELECT token FROM invitations WHERE id = $1`, invitation.ID)
	if err != nil {
		t.Fatalf("failed to fetch invitation token from db: %v", err)
	}

	// 2. Initiate signup with invitation token
	var signupResp struct {
		Message     string `json:"message"`
		TenantID    string `json:"tenant_id"`
		RequiresOTP bool   `json:"requires_otp"`
	}
	resp = e.do(t, http.MethodPost, "/api/v1/auth/passwordless/signup/initiate", "", fiber.Map{
		"email":            inviteeEmail,
		"name":             "New User",
		"invitation_token": invitationToken,
	}, &signupResp)
	assertStatus(t, resp, http.StatusCreated)
	if !signupResp.RequiresOTP {
		t.Fatal("expected requires_otp to be true")
	}

	// 3. Fetch OTP from database
	var otpCode string
	err = e.container.DB.Get(&otpCode,
		`SELECT code FROM otps WHERE contact = $1 ORDER BY created_at DESC LIMIT 1`,
		inviteeEmail)
	if err != nil {
		t.Fatalf("failed to fetch OTP from db: %v", err)
	}

	// 4. Verify signup
	resp = e.do(t, http.MethodPost, "/api/v1/auth/passwordless/signup/verify", "", fiber.Map{
		"email":     inviteeEmail,
		"code":      otpCode,
		"tenant_id": admin.tenantID,
	}, nil)
	assertStatus(t, resp, http.StatusOK)

	// 5. Initiate login
	resp = e.do(t, http.MethodPost, "/api/v1/auth/passwordless/login/initiate", "", fiber.Map{
		"email":     inviteeEmail,
		"tenant_id": admin.tenantID,
	}, nil)
	assertStatus(t, resp, http.StatusOK)

	// 6. Fetch login OTP
	// Wait briefly for rate limiting
	time.Sleep(100 * time.Millisecond)
	var loginOTP string
	err = e.container.DB.Get(&loginOTP,
		`SELECT code FROM otps WHERE contact = $1 ORDER BY created_at DESC LIMIT 1`,
		inviteeEmail)
	if err != nil {
		t.Fatalf("failed to fetch login OTP: %v", err)
	}

	// 7. Verify login - should return tokens
	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		User         struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
	}
	resp = e.do(t, http.MethodPost, "/api/v1/auth/passwordless/login/verify", "", fiber.Map{
		"email":     inviteeEmail,
		"code":      loginOTP,
		"tenant_id": admin.tenantID,
	}, &tokenResp)
	assertStatus(t, resp, http.StatusOK)
	if tokenResp.AccessToken == "" {
		t.Fatal("expected access_token")
	}
	if tokenResp.RefreshToken == "" {
		t.Fatal("expected refresh_token")
	}
	if tokenResp.TokenType != "Bearer" {
		t.Fatalf("expected token_type Bearer, got %s", tokenResp.TokenType)
	}

	// 8. Use token to get current user
	var meResp struct {
		User struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		} `json:"user"`
		Tenant struct {
			CompanyName string `json:"company_name"`
		} `json:"tenant"`
	}
	resp = e.do(t, http.MethodGet, "/api/v1/auth/me", tokenResp.AccessToken, nil, &meResp)
	assertStatus(t, resp, http.StatusOK)
	if meResp.User.Email != inviteeEmail {
		t.Fatalf("expected email %s, got %s", inviteeEmail, meResp.User.Email)
	}

	// 9. Refresh token
	var refreshResp struct {
		AccessToken string `json:"access_token"`
	}
	resp = e.do(t, http.MethodPost, "/api/v1/auth/refresh", "", fiber.Map{
		"refresh_token": tokenResp.RefreshToken,
	}, &refreshResp)
	assertStatus(t, resp, http.StatusOK)
	if refreshResp.AccessToken == "" {
		t.Fatal("expected new access_token after refresh")
	}

	// 10. Logout
	resp = e.do(t, http.MethodPost, "/api/v1/auth/logout", tokenResp.AccessToken, fiber.Map{
		"refresh_token": tokenResp.RefreshToken,
	}, nil)
	assertStatus(t, resp, http.StatusOK)
}

func TestAuthValidation(t *testing.T) {
	e := setupE2E(t)

	// Signup with invalid email
	resp := e.do(t, http.MethodPost, "/api/v1/auth/passwordless/signup/initiate", "", fiber.Map{
		"email":            "not-an-email",
		"name":             "Test",
		"invitation_token": "some-token",
	}, nil)
	assertStatus(t, resp, http.StatusBadRequest)

	// Signup with short name
	resp = e.do(t, http.MethodPost, "/api/v1/auth/passwordless/signup/initiate", "", fiber.Map{
		"email":            "valid@test.com",
		"name":             "X",
		"invitation_token": "some-token",
	}, nil)
	assertStatus(t, resp, http.StatusBadRequest)

	// Signup without invitation token
	resp = e.do(t, http.MethodPost, "/api/v1/auth/passwordless/signup/initiate", "", fiber.Map{
		"email": "valid@test.com",
		"name":  "Valid Name",
	}, nil)
	assertStatus(t, resp, http.StatusBadRequest)

	// Login without tenant_id
	resp = e.do(t, http.MethodPost, "/api/v1/auth/passwordless/login/initiate", "", fiber.Map{
		"email": "valid@test.com",
	}, nil)
	assertStatus(t, resp, http.StatusBadRequest)
}

func TestUnauthorizedAccess(t *testing.T) {
	e := setupE2E(t)

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/users"},
		{http.MethodGet, "/api/v1/roles"},
		{http.MethodGet, "/api/v1/tenants"},
		{http.MethodGet, "/api/v1/invitations"},
		{http.MethodGet, "/api/v1/api-keys"},
		{http.MethodGet, "/api/v1/scopes"},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			resp := e.do(t, ep.method, ep.path, "", nil, nil)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("expected 401 for %s %s without token, got %d", ep.method, ep.path, resp.StatusCode)
			}
		})
	}
}

// ============================================================================
// Role CRUD Tests
// ============================================================================

func TestRoleLifecycle(t *testing.T) {
	e := setupE2E(t)
	admin := seedAdmin(t, e, "Role E2E Co")

	// Create role
	var created struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Scopes      []string `json:"scopes"`
	}
	resp := e.do(t, http.MethodPost, "/api/v1/roles", admin.token, fiber.Map{
		"name":        "Editor",
		"description": "Can edit things",
		"scopes":      []string{"users:read", "users:write"},
	}, &created)
	assertStatus(t, resp, http.StatusCreated)
	if created.Name != "Editor" {
		t.Fatalf("expected name Editor, got %s", created.Name)
	}
	if len(created.Scopes) != 2 {
		t.Fatalf("expected 2 scopes, got %d", len(created.Scopes))
	}

	// List roles
	var roleList struct {
		Roles []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"roles"`
		Total int `json:"total"`
	}
	resp = e.do(t, http.MethodGet, "/api/v1/roles", admin.token, nil, &roleList)
	assertStatus(t, resp, http.StatusOK)
	if len(roleList.Roles) == 0 {
		t.Fatal("expected at least 1 role")
	}

	// Get role by ID
	var fetched struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	resp = e.do(t, http.MethodGet, "/api/v1/roles/"+created.ID, admin.token, nil, &fetched)
	assertStatus(t, resp, http.StatusOK)
	if fetched.Name != "Editor" {
		t.Fatalf("expected name Editor, got %s", fetched.Name)
	}

	// Update role
	var updated struct {
		Name string `json:"name"`
	}
	newName := "Senior Editor"
	resp = e.do(t, http.MethodPut, "/api/v1/roles/"+created.ID, admin.token, fiber.Map{
		"name": &newName,
	}, &updated)
	assertStatus(t, resp, http.StatusOK)
	if updated.Name != "Senior Editor" {
		t.Fatalf("expected updated name Senior Editor, got %s", updated.Name)
	}

	// Assign role to admin user
	resp = e.do(t, http.MethodPost, "/api/v1/roles/"+created.ID+"/assign", admin.token, fiber.Map{
		"user_id": admin.userID,
	}, nil)
	assertStatus(t, resp, http.StatusOK)

	// Get user roles
	var userRolesResp struct {
		Roles []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"roles"`
	}
	resp = e.do(t, http.MethodGet, "/api/v1/users/"+admin.userID+"/roles", admin.token, nil, &userRolesResp)
	assertStatus(t, resp, http.StatusOK)
	if len(userRolesResp.Roles) == 0 {
		t.Fatal("expected at least 1 user role")
	}

	// Unassign role
	resp = e.do(t, http.MethodDelete, "/api/v1/roles/"+created.ID+"/users/"+admin.userID, admin.token, nil, nil)
	assertStatus(t, resp, http.StatusOK)

	// Delete role
	resp = e.do(t, http.MethodDelete, "/api/v1/roles/"+created.ID, admin.token, nil, nil)
	assertStatus(t, resp, http.StatusOK)
}

func TestRoleValidation(t *testing.T) {
	e := setupE2E(t)
	admin := seedAdmin(t, e, "Role Validation Co")

	// Name too short
	resp := e.do(t, http.MethodPost, "/api/v1/roles", admin.token, fiber.Map{
		"name":   "X",
		"scopes": []string{"users:read"},
	}, nil)
	assertStatus(t, resp, http.StatusBadRequest)

	// No scopes
	resp = e.do(t, http.MethodPost, "/api/v1/roles", admin.token, fiber.Map{
		"name": "Valid Name",
	}, nil)
	assertStatus(t, resp, http.StatusBadRequest)
}

// ============================================================================
// Invitation Tests
// ============================================================================

func TestInvitationLifecycle(t *testing.T) {
	e := setupE2E(t)
	admin := seedAdmin(t, e, "Invitation E2E Co")

	inviteeEmail := fmt.Sprintf("invite-%s@e2e-test.example.com", uuid.NewString()[:8])

	// Create invitation
	var created struct {
		ID       string   `json:"id"`
		Email    string   `json:"email"`
		Status   string   `json:"status"`
		Scopes   []string `json:"scopes"`
		TenantID string   `json:"tenant_id"`
	}
	resp := e.do(t, http.MethodPost, "/api/v1/invitations", admin.token, fiber.Map{
		"email":  inviteeEmail,
		"scopes": []string{"users:read"},
	}, &created)
	assertStatus(t, resp, http.StatusCreated)
	if created.Email != inviteeEmail {
		t.Fatalf("expected email %s, got %s", inviteeEmail, created.Email)
	}
	if created.Status != "PENDING" {
		t.Fatalf("expected status PENDING, got %s", created.Status)
	}

	// Fetch token from DB (not exposed in API for security)
	var invToken string
	err := e.container.DB.Get(&invToken, `SELECT token FROM invitations WHERE id = $1`, created.ID)
	if err != nil {
		t.Fatalf("failed to fetch invitation token: %v", err)
	}

	// List tenant invitations
	var listResp struct {
		Invitations []struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"invitations"`
	}
	resp = e.do(t, http.MethodGet, "/api/v1/invitations", admin.token, nil, &listResp)
	assertStatus(t, resp, http.StatusOK)
	if len(listResp.Invitations) == 0 {
		t.Fatal("expected at least 1 invitation")
	}

	// Get pending invitations
	resp = e.do(t, http.MethodGet, "/api/v1/invitations/pending", admin.token, nil, nil)
	assertStatus(t, resp, http.StatusOK)

	// Get invitation by ID
	resp = e.do(t, http.MethodGet, "/api/v1/invitations/"+created.ID, admin.token, nil, nil)
	assertStatus(t, resp, http.StatusOK)

	// Note: public invitation routes (/invitations/public/*) are not tested here
	// because they're behind the /invitations auth middleware group in Fiber.
	// This is a known routing issue to fix in invitationapi.RegisterRoutes.

	// Revoke invitation
	resp = e.do(t, http.MethodPost, "/api/v1/invitations/"+created.ID+"/revoke", admin.token, nil, nil)
	assertStatus(t, resp, http.StatusOK)

	// Create another and delete it
	var toDelete struct {
		ID string `json:"id"`
	}
	resp = e.do(t, http.MethodPost, "/api/v1/invitations", admin.token, fiber.Map{
		"email":  fmt.Sprintf("delete-%s@e2e-test.example.com", uuid.NewString()[:8]),
		"scopes": []string{"users:read"},
	}, &toDelete)
	assertStatus(t, resp, http.StatusCreated)

	resp = e.do(t, http.MethodDelete, "/api/v1/invitations/"+toDelete.ID, admin.token, nil, nil)
	assertStatus(t, resp, http.StatusOK)
}

func TestInvitationValidation(t *testing.T) {
	e := setupE2E(t)
	admin := seedAdmin(t, e, "Invite Validation Co")

	// Invalid email
	resp := e.do(t, http.MethodPost, "/api/v1/invitations", admin.token, fiber.Map{
		"email": "not-an-email",
	}, nil)
	assertStatus(t, resp, http.StatusBadRequest)
}

// ============================================================================
// User CRUD Tests
// ============================================================================

func TestUserLifecycle(t *testing.T) {
	e := setupE2E(t)
	admin := seedAdmin(t, e, "User E2E Co")

	// Create user
	var createdUserResp struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	newEmail := fmt.Sprintf("user-%s@e2e-test.example.com", uuid.NewString()[:8])
	resp := e.do(t, http.MethodPost, "/api/v1/users", admin.token, fiber.Map{
		"email":  newEmail,
		"name":   "Test User",
		"scopes": []string{"users:read"},
	}, &createdUserResp)
	assertStatus(t, resp, http.StatusCreated)
	if createdUserResp.Email != newEmail {
		t.Fatalf("expected email %s, got %s", newEmail, createdUserResp.Email)
	}

	// List users
	resp = e.do(t, http.MethodGet, "/api/v1/users", admin.token, nil, nil)
	assertStatus(t, resp, http.StatusOK)

	// Get user by ID
	var fetchedUser struct {
		User struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"user"`
	}
	resp = e.do(t, http.MethodGet, "/api/v1/users/"+createdUserResp.ID, admin.token, nil, &fetchedUser)
	assertStatus(t, resp, http.StatusOK)
	if fetchedUser.User.Name != "Test User" {
		t.Fatalf("expected name Test User, got %s", fetchedUser.User.Name)
	}

	// Update user
	newName := "Updated User"
	resp = e.do(t, http.MethodPut, "/api/v1/users/"+createdUserResp.ID, admin.token, fiber.Map{
		"name": &newName,
	}, nil)
	assertStatus(t, resp, http.StatusOK)

	// Activate user first (created user is PENDING)
	resp = e.do(t, http.MethodPost, "/api/v1/users/"+createdUserResp.ID+"/activate", admin.token, nil, nil)
	assertStatus(t, resp, http.StatusOK)

	// Suspend user
	resp = e.do(t, http.MethodPost, "/api/v1/users/"+createdUserResp.ID+"/suspend", admin.token, fiber.Map{
		"reason": "Testing suspension",
	}, nil)
	assertStatus(t, resp, http.StatusOK)

	// Delete user (can delete from any status)
	resp = e.do(t, http.MethodDelete, "/api/v1/users/"+createdUserResp.ID, admin.token, nil, nil)
	assertStatus(t, resp, http.StatusOK)
}

func TestUserValidation(t *testing.T) {
	e := setupE2E(t)
	admin := seedAdmin(t, e, "User Validation Co")

	// Invalid email
	resp := e.do(t, http.MethodPost, "/api/v1/users", admin.token, fiber.Map{
		"email": "bad",
		"name":  "Valid Name",
	}, nil)
	assertStatus(t, resp, http.StatusBadRequest)

	// Short name
	resp = e.do(t, http.MethodPost, "/api/v1/users", admin.token, fiber.Map{
		"email": "valid@test.com",
		"name":  "X",
	}, nil)
	assertStatus(t, resp, http.StatusBadRequest)
}

// ============================================================================
// Scope Management Tests
// ============================================================================

func TestScopeManagement(t *testing.T) {
	e := setupE2E(t)
	admin := seedAdmin(t, e, "Scope E2E Co")

	// Get all available scopes
	resp := e.do(t, http.MethodGet, "/api/v1/scopes", admin.token, nil, nil)
	assertStatus(t, resp, http.StatusOK)

	// Create a user to manage scopes on
	var created struct {
		ID string `json:"id"`
	}
	resp = e.do(t, http.MethodPost, "/api/v1/users", admin.token, fiber.Map{
		"email":  fmt.Sprintf("scope-user-%s@test.com", uuid.NewString()[:8]),
		"name":   "Scope User",
		"scopes": []string{"users:read"},
	}, &created)
	assertStatus(t, resp, http.StatusCreated)

	// Get user scopes
	resp = e.do(t, http.MethodGet, "/api/v1/users/"+created.ID+"/scopes", admin.token, nil, nil)
	assertStatus(t, resp, http.StatusOK)

	// Add scopes
	resp = e.do(t, http.MethodPost, "/api/v1/users/"+created.ID+"/scopes", admin.token, fiber.Map{
		"scopes": []string{"roles:read", "roles:write"},
	}, nil)
	assertStatus(t, resp, http.StatusOK)

	// Set (replace) scopes
	resp = e.do(t, http.MethodPut, "/api/v1/users/"+created.ID+"/scopes", admin.token, fiber.Map{
		"scopes": []string{"users:read", "users:write", "roles:read"},
	}, nil)
	assertStatus(t, resp, http.StatusOK)

	// Remove scopes
	resp = e.do(t, http.MethodDelete, "/api/v1/users/"+created.ID+"/scopes", admin.token, fiber.Map{
		"scopes": []string{"roles:read"},
	}, nil)
	assertStatus(t, resp, http.StatusOK)
}

func TestScopeValidation(t *testing.T) {
	e := setupE2E(t)
	admin := seedAdmin(t, e, "Scope Validation Co")

	// Create user
	var created struct {
		ID string `json:"id"`
	}
	resp := e.do(t, http.MethodPost, "/api/v1/users", admin.token, fiber.Map{
		"email":  fmt.Sprintf("sv-%s@test.com", uuid.NewString()[:8]),
		"name":   "Scope Val User",
		"scopes": []string{"users:read"},
	}, &created)
	assertStatus(t, resp, http.StatusCreated)

	// Empty scopes
	resp = e.do(t, http.MethodPost, "/api/v1/users/"+created.ID+"/scopes", admin.token, fiber.Map{
		"scopes": []string{},
	}, nil)
	assertStatus(t, resp, http.StatusBadRequest)
}

// ============================================================================
// Tenant CRUD Tests
// ============================================================================

func TestTenantLifecycle(t *testing.T) {
	e := setupE2E(t)
	admin := seedAdmin(t, e, "Tenant E2E Co")

	// Create tenant
	var created struct {
		ID          string `json:"id"`
		CompanyName string `json:"company_name"`
		Status      string `json:"status"`
	}
	resp := e.do(t, http.MethodPost, "/api/v1/tenants", admin.token, fiber.Map{
		"company_name": "New Tenant Inc",
	}, &created)
	assertStatus(t, resp, http.StatusCreated)
	if created.CompanyName != "New Tenant Inc" {
		t.Fatalf("expected company_name 'New Tenant Inc', got %s", created.CompanyName)
	}

	// List tenants
	resp = e.do(t, http.MethodGet, "/api/v1/tenants", admin.token, nil, nil)
	assertStatus(t, resp, http.StatusOK)

	// Get tenant by ID
	var fetched struct {
		ID          string `json:"id"`
		CompanyName string `json:"company_name"`
	}
	resp = e.do(t, http.MethodGet, "/api/v1/tenants/"+created.ID, admin.token, nil, &fetched)
	assertStatus(t, resp, http.StatusOK)

	// Update tenant
	newCompanyName := "Updated Tenant Inc"
	resp = e.do(t, http.MethodPut, "/api/v1/tenants/"+created.ID, admin.token, fiber.Map{
		"company_name": &newCompanyName,
	}, nil)
	assertStatus(t, resp, http.StatusOK)

	// Get tenant stats
	resp = e.do(t, http.MethodGet, "/api/v1/tenants/"+created.ID+"/stats", admin.token, nil, nil)
	assertStatus(t, resp, http.StatusOK)

	// Get tenant usage
	resp = e.do(t, http.MethodGet, "/api/v1/tenants/"+created.ID+"/usage", admin.token, nil, nil)
	assertStatus(t, resp, http.StatusOK)

	// Get tenant users
	resp = e.do(t, http.MethodGet, "/api/v1/tenants/"+created.ID+"/users", admin.token, nil, nil)
	assertStatus(t, resp, http.StatusOK)

	// Suspend tenant
	resp = e.do(t, http.MethodPost, "/api/v1/tenants/"+created.ID+"/suspend", admin.token, fiber.Map{
		"reason": "Suspending for E2E testing purposes",
	}, nil)
	assertStatus(t, resp, http.StatusOK)

	// Activate tenant
	resp = e.do(t, http.MethodPost, "/api/v1/tenants/"+created.ID+"/activate", admin.token, nil, nil)
	assertStatus(t, resp, http.StatusOK)

	// Upgrade plan
	resp = e.do(t, http.MethodPost, "/api/v1/tenants/"+created.ID+"/upgrade", admin.token, fiber.Map{
		"new_plan": "PROFESSIONAL",
	}, nil)
	assertStatus(t, resp, http.StatusOK)

	// Tenant config
	resp = e.do(t, http.MethodPut, "/api/v1/tenants/"+created.ID+"/config", admin.token, fiber.Map{
		"key":   "feature_flag",
		"value": "enabled",
	}, nil)
	assertStatus(t, resp, http.StatusOK)

	resp = e.do(t, http.MethodGet, "/api/v1/tenants/"+created.ID+"/config", admin.token, nil, nil)
	assertStatus(t, resp, http.StatusOK)

	resp = e.do(t, http.MethodDelete, "/api/v1/tenants/"+created.ID+"/config/feature_flag", admin.token, nil, nil)
	assertStatus(t, resp, http.StatusOK)

	// Delete tenant
	resp = e.do(t, http.MethodDelete, "/api/v1/tenants/"+created.ID, admin.token, nil, nil)
	assertStatus(t, resp, http.StatusOK)
}

func TestTenantValidation(t *testing.T) {
	e := setupE2E(t)
	admin := seedAdmin(t, e, "Tenant Validation Co")

	// Short company name
	resp := e.do(t, http.MethodPost, "/api/v1/tenants", admin.token, fiber.Map{
		"company_name": "X",
	}, nil)
	assertStatus(t, resp, http.StatusBadRequest)

	// Create a tenant to test suspend validation
	var created struct {
		ID string `json:"id"`
	}
	resp = e.do(t, http.MethodPost, "/api/v1/tenants", admin.token, fiber.Map{
		"company_name": "Suspend Test Co",
	}, &created)
	assertStatus(t, resp, http.StatusCreated)

	// Suspend with short reason (min 10 chars)
	resp = e.do(t, http.MethodPost, "/api/v1/tenants/"+created.ID+"/suspend", admin.token, fiber.Map{
		"reason": "short",
	}, nil)
	assertStatus(t, resp, http.StatusBadRequest)

	// Invalid upgrade plan
	resp = e.do(t, http.MethodPost, "/api/v1/tenants/"+created.ID+"/upgrade", admin.token, fiber.Map{
		"new_plan": "INVALID_PLAN",
	}, nil)
	assertStatus(t, resp, http.StatusBadRequest)
}

// ============================================================================
// API Key Tests
// ============================================================================

func TestAPIKeyLifecycle(t *testing.T) {
	e := setupE2E(t)
	admin := seedAdmin(t, e, "APIKey E2E Co")

	// Create API key
	var createdKey struct {
		APIKey struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Environment string `json:"environment"`
		} `json:"api_key"`
		SecretKey string `json:"secret_key"`
	}
	resp := e.do(t, http.MethodPost, "/api/v1/api-keys", admin.token, fiber.Map{
		"name":        "Test Key",
		"description": "For testing",
		"scopes":      []string{"users:read"},
		"environment": "test",
	}, &createdKey)
	assertStatus(t, resp, http.StatusCreated)
	if createdKey.SecretKey == "" {
		t.Fatal("expected secret_key to be returned on creation")
	}
	if createdKey.APIKey.Name != "Test Key" {
		t.Fatalf("expected name 'Test Key', got %s", createdKey.APIKey.Name)
	}

	// List API keys
	resp = e.do(t, http.MethodGet, "/api/v1/api-keys", admin.token, nil, nil)
	assertStatus(t, resp, http.StatusOK)

	// Get API key by ID
	resp = e.do(t, http.MethodGet, "/api/v1/api-keys/"+createdKey.APIKey.ID, admin.token, nil, nil)
	assertStatus(t, resp, http.StatusOK)

	// Update API key
	newName := "Updated Key"
	resp = e.do(t, http.MethodPut, "/api/v1/api-keys/"+createdKey.APIKey.ID, admin.token, fiber.Map{
		"name": &newName,
	}, nil)
	assertStatus(t, resp, http.StatusOK)

	// Use API key for authentication
	resp = e.do(t, http.MethodGet, "/api/v1/users", "", nil, nil)
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/users", nil)
	req.Header.Set("X-API-Key", createdKey.SecretKey)
	apiKeyResp, err := e.app.Test(req, -1)
	if err != nil {
		t.Fatalf("API key request failed: %v", err)
	}
	apiKeyResp.Body.Close()
	// API key should authenticate (may not have scope, but should not be 401)
	if apiKeyResp.StatusCode == http.StatusUnauthorized {
		t.Fatal("expected API key to authenticate")
	}

	// Revoke API key
	resp = e.do(t, http.MethodPost, "/api/v1/api-keys/"+createdKey.APIKey.ID+"/revoke", admin.token, nil, nil)
	assertStatus(t, resp, http.StatusOK)

	// Delete API key
	resp = e.do(t, http.MethodDelete, "/api/v1/api-keys/"+createdKey.APIKey.ID, admin.token, nil, nil)
	assertStatus(t, resp, http.StatusOK)
}

func TestAPIKeyValidation(t *testing.T) {
	e := setupE2E(t)
	admin := seedAdmin(t, e, "APIKey Validation Co")

	// Short name (min 3)
	resp := e.do(t, http.MethodPost, "/api/v1/api-keys", admin.token, fiber.Map{
		"name":        "AB",
		"scopes":      []string{"users:read"},
		"environment": "test",
	}, nil)
	assertStatus(t, resp, http.StatusBadRequest)

	// No scopes
	resp = e.do(t, http.MethodPost, "/api/v1/api-keys", admin.token, fiber.Map{
		"name":        "Valid Name",
		"environment": "test",
	}, nil)
	assertStatus(t, resp, http.StatusBadRequest)

	// Invalid environment
	resp = e.do(t, http.MethodPost, "/api/v1/api-keys", admin.token, fiber.Map{
		"name":        "Valid Name",
		"scopes":      []string{"users:read"},
		"environment": "staging",
	}, nil)
	assertStatus(t, resp, http.StatusBadRequest)
}

// ============================================================================
// Cross-Cutting Tests
// ============================================================================

func TestGetUserTenants(t *testing.T) {
	e := setupE2E(t)
	admin := seedAdmin(t, e, "Tenant Lookup Co")

	var tenantsResp struct {
		Email   string `json:"email"`
		Tenants []struct {
			TenantID    string `json:"tenant_id"`
			CompanyName string `json:"company_name"`
		} `json:"tenants"`
		Count int `json:"count"`
	}
	resp := e.do(t, http.MethodPost, "/api/v1/auth/passwordless/tenants", "", fiber.Map{
		"email": admin.email,
	}, &tenantsResp)
	assertStatus(t, resp, http.StatusOK)
	if tenantsResp.Count == 0 {
		t.Fatal("expected at least 1 tenant for admin email")
	}
	if tenantsResp.Tenants[0].CompanyName != "Tenant Lookup Co" {
		t.Fatalf("expected company name 'Tenant Lookup Co', got %s", tenantsResp.Tenants[0].CompanyName)
	}
}

func TestHealthEndpoint(t *testing.T) {
	e := setupE2E(t)

	var health struct {
		Status string `json:"status"`
		DB     string `json:"db"`
		Redis  string `json:"redis"`
	}
	resp := e.do(t, http.MethodGet, "/health", "", nil, &health)
	assertStatus(t, resp, http.StatusOK)
	if health.Status != "healthy" {
		t.Fatalf("expected healthy status, got %s", health.Status)
	}
	if health.DB != "healthy" {
		t.Fatalf("expected db healthy, got %s", health.DB)
	}
	if health.Redis != "healthy" {
		t.Fatalf("expected redis healthy, got %s", health.Redis)
	}
}
