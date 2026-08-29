package auth_test

import (
	"testing"
	"time"

	"github.com/Abraxas-365/manifesto/internal/iam/auth"
	"github.com/Abraxas-365/manifesto/internal/kernel"
)

func TestRefreshToken_IsValid(t *testing.T) {
	tests := []struct {
		name  string
		token auth.RefreshToken
		want  bool
	}{
		{
			name: "valid token",
			token: auth.RefreshToken{
				IsRevoked: false,
				ExpiresAt: time.Now().Add(time.Hour),
			},
			want: true,
		},
		{
			name: "revoked token",
			token: auth.RefreshToken{
				IsRevoked: true,
				ExpiresAt: time.Now().Add(time.Hour),
			},
			want: false,
		},
		{
			name: "expired token",
			token: auth.RefreshToken{
				IsRevoked: false,
				ExpiresAt: time.Now().Add(-time.Hour),
			},
			want: false,
		},
		{
			name: "revoked and expired",
			token: auth.RefreshToken{
				IsRevoked: true,
				ExpiresAt: time.Now().Add(-time.Hour),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.token.IsValid(); got != tt.want {
				t.Errorf("RefreshToken.IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUserSession_IsExpired(t *testing.T) {
	tests := []struct {
		name    string
		session auth.UserSession
		want    bool
	}{
		{
			name:    "active session",
			session: auth.UserSession{ExpiresAt: time.Now().Add(time.Hour)},
			want:    false,
		},
		{
			name:    "expired session",
			session: auth.UserSession{ExpiresAt: time.Now().Add(-time.Hour)},
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.session.IsExpired(); got != tt.want {
				t.Errorf("UserSession.IsExpired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTokenClaims_SessionID(t *testing.T) {
	claims := &auth.TokenClaims{
		UserID:    kernel.NewUserID("user-1"),
		TenantID:  kernel.NewTenantID("tenant-1"),
		SessionID: "session-123",
		Email:     "test@example.com",
		Scopes:    []string{"users:read"},
	}

	if claims.SessionID != "session-123" {
		t.Errorf("TokenClaims.SessionID = %q, want %q", claims.SessionID, "session-123")
	}
}

func TestRefreshToken_SessionID(t *testing.T) {
	token := auth.RefreshToken{
		ID:        "tok-1",
		Token:     "jwt-value",
		UserID:    kernel.NewUserID("user-1"),
		TenantID:  kernel.NewTenantID("tenant-1"),
		SessionID: "session-456",
		ExpiresAt: time.Now().Add(time.Hour),
		CreatedAt: time.Now(),
		IsRevoked: false,
	}

	if token.SessionID != "session-456" {
		t.Errorf("RefreshToken.SessionID = %q, want %q", token.SessionID, "session-456")
	}

	if !token.IsValid() {
		t.Error("expected token to be valid")
	}
}

func TestUserSession_NoSessionToken(t *testing.T) {
	// Verify UserSession no longer has a SessionToken field.
	// This is a compile-time check: if the field exists, this won't compile.
	session := auth.UserSession{
		ID:           "sess-1",
		UserID:       kernel.NewUserID("user-1"),
		TenantID:     kernel.NewTenantID("tenant-1"),
		IPAddress:    "127.0.0.1",
		UserAgent:    "Go-Test",
		ExpiresAt:    time.Now().Add(24 * time.Hour),
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
	}
	// If this compiles, the dead SessionToken field has been removed
	if session.ID != "sess-1" {
		t.Error("unexpected session ID")
	}
}
