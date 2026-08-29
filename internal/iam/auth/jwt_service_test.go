package auth_test

import (
	"testing"
	"time"

	"github.com/Abraxas-365/manifesto/internal/config"
	"github.com/Abraxas-365/manifesto/internal/iam/auth"
	"github.com/Abraxas-365/manifesto/internal/kernel"
)

func newTestJWTService() *auth.JWTService {
	return auth.NewJWTServiceFromConfig(&config.JWTConfig{
		SecretKey:       "test-secret-key-at-least-32-bytes-long",
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 7 * 24 * time.Hour,
		Issuer:          "test",
		Audience:        []string{"test-api"},
	})
}

func TestJWTService_SessionIDInAccessToken(t *testing.T) {
	svc := newTestJWTService()

	userID := kernel.NewUserID("user-123")
	tenantID := kernel.NewTenantID("tenant-456")
	sessionID := "session-789"

	tokenStr, err := svc.GenerateAccessToken(userID, tenantID, map[string]any{
		"email":      "test@example.com",
		"name":       "Test User",
		"scopes":     []string{"users:read", "roles:write"},
		"session_id": sessionID,
	})
	if err != nil {
		t.Fatalf("GenerateAccessToken: %v", err)
	}

	claims, err := svc.ValidateAccessToken(tokenStr)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}

	if claims.SessionID != sessionID {
		t.Errorf("claims.SessionID = %q, want %q", claims.SessionID, sessionID)
	}
	if claims.UserID != userID {
		t.Errorf("claims.UserID = %v, want %v", claims.UserID, userID)
	}
	if claims.TenantID != tenantID {
		t.Errorf("claims.TenantID = %v, want %v", claims.TenantID, tenantID)
	}
	if claims.Email != "test@example.com" {
		t.Errorf("claims.Email = %q, want %q", claims.Email, "test@example.com")
	}
	if len(claims.Scopes) != 2 {
		t.Errorf("len(claims.Scopes) = %d, want 2", len(claims.Scopes))
	}
}

func TestJWTService_NoSessionID(t *testing.T) {
	svc := newTestJWTService()

	tokenStr, err := svc.GenerateAccessToken(
		kernel.NewUserID("user-1"),
		kernel.NewTenantID("tenant-1"),
		map[string]any{
			"email":  "test@example.com",
			"name":   "Test",
			"scopes": []string{},
		},
	)
	if err != nil {
		t.Fatalf("GenerateAccessToken: %v", err)
	}

	claims, err := svc.ValidateAccessToken(tokenStr)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}

	if claims.SessionID != "" {
		t.Errorf("claims.SessionID = %q, want empty string", claims.SessionID)
	}
}

func TestJWTService_ExpiredToken(t *testing.T) {
	svc := auth.NewJWTServiceFromConfig(&config.JWTConfig{
		SecretKey:       "test-secret-key-at-least-32-bytes-long",
		AccessTokenTTL:  -time.Hour, // already expired
		RefreshTokenTTL: 7 * 24 * time.Hour,
		Issuer:          "test",
		Audience:        []string{"test-api"},
	})

	tokenStr, err := svc.GenerateAccessToken(
		kernel.NewUserID("user-1"),
		kernel.NewTenantID("tenant-1"),
		map[string]any{
			"email":      "test@example.com",
			"name":       "Test",
			"scopes":     []string{},
			"session_id": "session-expired",
		},
	)
	if err != nil {
		t.Fatalf("GenerateAccessToken: %v", err)
	}

	_, err = svc.ValidateAccessToken(tokenStr)
	if err == nil {
		t.Error("expected error for expired token, got nil")
	}
}

func TestJWTService_RefreshToken(t *testing.T) {
	svc := newTestJWTService()

	tokenStr, err := svc.GenerateRefreshToken(kernel.NewUserID("user-1"))
	if err != nil {
		t.Fatalf("GenerateRefreshToken: %v", err)
	}

	if tokenStr == "" {
		t.Error("expected non-empty refresh token")
	}
}

func TestAuthContext_SessionID(t *testing.T) {
	userID := kernel.NewUserID("user-1")
	ctx := &kernel.AuthContext{
		UserID:    &userID,
		TenantID:  kernel.NewTenantID("tenant-1"),
		SessionID: "session-abc",
		Email:     "test@example.com",
		Scopes:    []string{"users:read"},
		IsAPIKey:  false,
	}

	if !ctx.IsValid() {
		t.Error("expected AuthContext to be valid")
	}

	if ctx.SessionID != "session-abc" {
		t.Errorf("AuthContext.SessionID = %q, want %q", ctx.SessionID, "session-abc")
	}
}

func TestAuthContext_APIKeyNoSessionID(t *testing.T) {
	ctx := &kernel.AuthContext{
		TenantID: kernel.NewTenantID("tenant-1"),
		Scopes:   []string{"users:read"},
		IsAPIKey: true,
	}

	if !ctx.IsValid() {
		t.Error("expected API key AuthContext to be valid")
	}

	if ctx.SessionID != "" {
		t.Errorf("API key AuthContext should have empty SessionID, got %q", ctx.SessionID)
	}
}
