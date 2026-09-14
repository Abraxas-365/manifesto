package auth_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Abraxas-365/manifesto/internal/errx"
	"github.com/Abraxas-365/manifesto/internal/iam/auth"
	"github.com/Abraxas-365/manifesto/internal/iam/tenant"
	"github.com/Abraxas-365/manifesto/internal/iam/user"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"github.com/gofiber/fiber/v2"
)

func middlewareTestApp() *fiber.App {
	return fiber.New(fiber.Config{ErrorHandler: func(c *fiber.Ctx, err error) error {
		if e, ok := err.(*errx.Error); ok {
			return c.SendStatus(e.HTTPStatus)
		}
		return fiber.DefaultErrorHandler(c, err)
	}})
}

func TestUserJWTMiddleware(t *testing.T) {
	svc := newTestJWTService()
	token := func(session string) string {
		value, err := svc.GenerateAccessToken("user", "tenant", map[string]any{"session_id": session, "scopes": []string{"*"}})
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	valid, legacy := token("active"), token("")
	for _, tc := range []struct {
		name, header, cookie, apiKey, query string
		want                                int
	}{
		{name: "bearer", header: "Bearer " + valid, want: 204},
		{name: "custom cookie", cookie: valid, want: 204},
		{name: "no session ID compatibility", header: "Bearer " + legacy, want: 204},
		{name: "missing", want: 401},
		{name: "invalid bearer overrides cookie", header: "Bearer invalid", cookie: valid, want: 401},
		{name: "valid bearer overrides cookie", header: "Bearer " + valid, cookie: "invalid", want: 204},
		{name: "API key alone", apiKey: "key", want: 401},
		{name: "query key alone", query: "?api_key=key", want: 401},
		{name: "independent JWT cookie", apiKey: "key", cookie: valid, want: 204},
		{name: "revoked session", header: "Bearer " + token("missing"), want: 401},
		{name: "expired session", header: "Bearer " + token("expired"), want: 401},
		{name: "wrong user session", header: "Bearer " + token("other-user"), want: 401},
		{name: "wrong tenant session", header: "Bearer " + token("other-tenant"), want: 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMockSessionRepo()
			for _, s := range []auth.UserSession{
				{ID: "active", UserID: "user", TenantID: "tenant", ExpiresAt: time.Now().Add(time.Hour)},
				{ID: "expired", UserID: "user", TenantID: "tenant", ExpiresAt: time.Now().Add(-time.Hour)},
				{ID: "other-user", UserID: "other", TenantID: "tenant", ExpiresAt: time.Now().Add(time.Hour)},
				{ID: "other-tenant", UserID: "user", TenantID: "other", ExpiresAt: time.Now().Add(time.Hour)},
			} {
				if err := repo.SaveSession(context.Background(), s); err != nil {
					t.Fatal(err)
				}
			}
			middleware := auth.NewAPIKeyMiddleware(nil, svc, repo, "session_jwt", eligibleUserRepo{}, eligibleTenantRepo{})
			app := middlewareTestApp()
			app.Use(func(c *fiber.Ctx) error {
				c.Locals("auth", &kernel.AuthContext{TenantID: "tenant", Actor: kernel.NewAPIKeyActor(kernel.NewAPIKeyID("key")), Scopes: []string{"*"}})
				return c.Next()
			})
			app.Get("/session", middleware.AuthenticateUserJWT(), func(c *fiber.Ctx) error {
				actor, ok := auth.GetAuthContext(c)
				if !ok {
					t.Fatal("missing session identity")
				}
				userID, isUser := actor.Actor.UserID()
				if !isUser || len(actor.Scopes) != 0 || userID != "user" {
					t.Error("incorrect session identity")
				}
				if tc.header != "Bearer "+legacy && actor.SessionID != "active" {
					t.Error("session ID lost")
				}
				return c.SendStatus(204)
			})
			req := httptest.NewRequest("GET", "/session"+tc.query, nil)
			req.Header.Set("Authorization", tc.header)
			req.Header.Set("X-API-Key", tc.apiKey)
			if tc.cookie != "" {
				req.Header.Set("Cookie", "session_jwt="+tc.cookie)
			}
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Fatalf("status=%d want=%d", resp.StatusCode, tc.want)
			}
		})
	}
	// The public application middleware retains JWT scopes and uses the same cookie.
	app := middlewareTestApp()
	middleware := auth.NewAPIKeyMiddleware(nil, svc, newMockSessionRepo(), "session_jwt", eligibleUserRepo{}, eligibleTenantRepo{})
	app.Get("/resource", middleware.Authenticate(), middleware.RequireScope("users:read"), func(c *fiber.Ctx) error { return c.SendStatus(204) })
	req := httptest.NewRequest("GET", "/resource", nil)
	req.Header.Set("Cookie", "session_jwt="+legacy)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Fatalf("application status=%d", resp.StatusCode)
	}
}

func TestSessionRoutesRequireFreshJWT(t *testing.T) {
	app := middlewareTestApp()
	// Preexisting locals must not allow bypassing explicit route authentication.
	userID := kernel.NewUserID("user")
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("auth", &kernel.AuthContext{Actor: kernel.NewUserActor(userID), TenantID: "tenant"})
		return c.Next()
	})
	handlers := &auth.AuthHandlers{}
	handlers.RegisterRoutes(app.Group("/api/v1"), auth.NewAPIKeyMiddleware(nil, newTestJWTService(), newMockSessionRepo(), "session_jwt", eligibleUserRepo{}, eligibleTenantRepo{}))
	for _, tc := range []struct{ method, path string }{{"GET", "/me"}, {"GET", "/sessions"}, {"POST", "/logout"}, {"POST", "/logout/all"}} {
		resp, err := app.Test(httptest.NewRequest(tc.method, "/api/v1/auth"+tc.path, nil))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Errorf("%s status=%d", tc.path, resp.StatusCode)
		}
	}
}

// Existing middleware fixtures model an active, verified tenant membership.
type eligibleUserRepo struct{ user.UserRepository }

func (eligibleUserRepo) FindByID(_ context.Context, id kernel.UserID, tenantID kernel.TenantID) (*user.User, error) {
	return &user.User{ID: id, TenantID: tenantID, Status: user.UserStatusActive, EmailVerified: true}, nil
}

type eligibleTenantRepo struct{ tenant.TenantRepository }

func (eligibleTenantRepo) FindByID(_ context.Context, id kernel.TenantID) (*tenant.Tenant, error) {
	return &tenant.Tenant{ID: id, Status: tenant.TenantStatusActive}, nil
}
