package auth_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Abraxas-365/manifesto/internal/errx"
	"github.com/Abraxas-365/manifesto/internal/iam/auth"
	"github.com/Abraxas-365/manifesto/internal/kernel"
)

// ============================================================================
// Mock Token Repository
// ============================================================================

type mockTokenRepo struct {
	mu     sync.Mutex
	tokens map[string]*auth.RefreshToken // keyed by token value
}

func newMockTokenRepo() *mockTokenRepo {
	return &mockTokenRepo{tokens: make(map[string]*auth.RefreshToken)}
}

func (m *mockTokenRepo) SaveRefreshToken(_ context.Context, token auth.RefreshToken) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[token.Token] = &token
	return nil
}

func (m *mockTokenRepo) FindRefreshToken(_ context.Context, tokenValue string) (*auth.RefreshToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tokens[tokenValue]
	if !ok {
		return nil, auth.ErrInvalidRefreshToken()
	}
	return t, nil
}

func (m *mockTokenRepo) RevokeRefreshToken(_ context.Context, tokenValue string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tokens[tokenValue]
	if !ok {
		return auth.ErrInvalidRefreshToken()
	}
	t.IsRevoked = true
	return nil
}

func (m *mockTokenRepo) RevokeRefreshTokensBySessionID(_ context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tokens {
		if t.SessionID == sessionID {
			t.IsRevoked = true
		}
	}
	return nil
}

func (m *mockTokenRepo) RevokeAllUserTokens(_ context.Context, userID kernel.UserID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tokens {
		if t.UserID == userID {
			t.IsRevoked = true
		}
	}
	return nil
}

func (m *mockTokenRepo) CleanExpiredTokens(_ context.Context) error {
	return nil
}

// activeTokensForSession returns the count of non-revoked tokens for a session
func (m *mockTokenRepo) activeTokensForSession(sessionID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, t := range m.tokens {
		if t.SessionID == sessionID && !t.IsRevoked {
			count++
		}
	}
	return count
}

// ============================================================================
// Mock Session Repository
// ============================================================================

type mockSessionRepo struct {
	mu       sync.Mutex
	sessions map[string]*auth.UserSession // keyed by session ID
}

func newMockSessionRepo() *mockSessionRepo {
	return &mockSessionRepo{sessions: make(map[string]*auth.UserSession)}
}

func (m *mockSessionRepo) SaveSession(_ context.Context, session auth.UserSession) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[session.ID] = &session
	return nil
}

func (m *mockSessionRepo) FindSession(_ context.Context, sessionID string) (*auth.UserSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[sessionID]
	if !ok {
		return nil, errx.New("session not found", errx.TypeNotFound)
	}
	return s, nil
}

func (m *mockSessionRepo) FindUserSessions(_ context.Context, userID kernel.UserID) ([]*auth.UserSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*auth.UserSession
	for _, s := range m.sessions {
		if s.UserID == userID && !s.IsExpired() {
			result = append(result, s)
		}
	}
	return result, nil
}

func (m *mockSessionRepo) CountActiveSessions(_ context.Context, userID kernel.UserID) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, s := range m.sessions {
		if s.UserID == userID && !s.IsExpired() {
			count++
		}
	}
	return count, nil
}

func (m *mockSessionRepo) FindOldestSession(_ context.Context, userID kernel.UserID) (*auth.UserSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var oldest *auth.UserSession
	for _, s := range m.sessions {
		if s.UserID == userID && !s.IsExpired() {
			if oldest == nil || s.LastActivity.Before(oldest.LastActivity) {
				oldest = s
			}
		}
	}
	if oldest == nil {
		return nil, errx.New("no active sessions", errx.TypeNotFound)
	}
	return oldest, nil
}

func (m *mockSessionRepo) UpdateSessionActivity(_ context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[sessionID]
	if !ok {
		return errx.New("session not found", errx.TypeNotFound)
	}
	s.LastActivity = time.Now()
	return nil
}

func (m *mockSessionRepo) RevokeSession(_ context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
	return nil
}

func (m *mockSessionRepo) RevokeAllUserSessions(_ context.Context, userID kernel.UserID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		if s.UserID == userID {
			delete(m.sessions, id)
		}
	}
	return nil
}

func (m *mockSessionRepo) CleanExpiredSessions(_ context.Context) error {
	return nil
}

// ============================================================================
// Tests: Refresh Token Rotation
// ============================================================================

func TestRefreshTokenRotation_OldTokenRevoked(t *testing.T) {
	tokenRepo := newMockTokenRepo()

	sessionID := "session-1"
	oldToken := auth.RefreshToken{
		ID:        "rt-1",
		Token:     "old-refresh-token",
		UserID:    kernel.NewUserID("user-1"),
		TenantID:  kernel.NewTenantID("tenant-1"),
		SessionID: sessionID,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		CreatedAt: time.Now(),
		IsRevoked: false,
	}
	tokenRepo.SaveRefreshToken(context.Background(), oldToken)

	// Simulate rotation: revoke old, save new
	tokenRepo.RevokeRefreshToken(context.Background(), "old-refresh-token")

	newToken := auth.RefreshToken{
		ID:        "rt-2",
		Token:     "new-refresh-token",
		UserID:    kernel.NewUserID("user-1"),
		TenantID:  kernel.NewTenantID("tenant-1"),
		SessionID: sessionID,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		CreatedAt: time.Now(),
		IsRevoked: false,
	}
	tokenRepo.SaveRefreshToken(context.Background(), newToken)

	// Old token should be revoked
	found, err := tokenRepo.FindRefreshToken(context.Background(), "old-refresh-token")
	if err != nil {
		t.Fatalf("FindRefreshToken: %v", err)
	}
	if !found.IsRevoked {
		t.Error("old refresh token should be revoked after rotation")
	}

	// New token should be active
	found, err = tokenRepo.FindRefreshToken(context.Background(), "new-refresh-token")
	if err != nil {
		t.Fatalf("FindRefreshToken: %v", err)
	}
	if found.IsRevoked {
		t.Error("new refresh token should not be revoked")
	}
	if found.SessionID != sessionID {
		t.Errorf("new token SessionID = %q, want %q", found.SessionID, sessionID)
	}
}

// ============================================================================
// Tests: Theft Detection
// ============================================================================

func TestTheftDetection_RevokedTokenRevokesSession(t *testing.T) {
	tokenRepo := newMockTokenRepo()
	sessionRepo := newMockSessionRepo()

	sessionID := "session-victim"
	userID := kernel.NewUserID("user-1")

	// Save a session
	sessionRepo.SaveSession(context.Background(), auth.UserSession{
		ID:           sessionID,
		UserID:       userID,
		TenantID:     kernel.NewTenantID("tenant-1"),
		ExpiresAt:    time.Now().Add(24 * time.Hour),
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
	})

	// Save a token (already used/revoked — simulating the legitimate holder already rotated)
	stolenToken := auth.RefreshToken{
		ID:        "rt-stolen",
		Token:     "stolen-token-value",
		UserID:    userID,
		TenantID:  kernel.NewTenantID("tenant-1"),
		SessionID: sessionID,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		CreatedAt: time.Now(),
		IsRevoked: true, // already used/rotated
	}
	tokenRepo.SaveRefreshToken(context.Background(), stolenToken)

	// Save the current legitimate token for the same session
	legitimateToken := auth.RefreshToken{
		ID:        "rt-legit",
		Token:     "legitimate-token-value",
		UserID:    userID,
		TenantID:  kernel.NewTenantID("tenant-1"),
		SessionID: sessionID,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
		CreatedAt: time.Now(),
		IsRevoked: false,
	}
	tokenRepo.SaveRefreshToken(context.Background(), legitimateToken)

	// Simulate theft detection: attacker presents the stolen (revoked) token
	found, _ := tokenRepo.FindRefreshToken(context.Background(), "stolen-token-value")
	if found.IsRevoked {
		// THEFT DETECTED: revoke entire session
		tokenRepo.RevokeRefreshTokensBySessionID(context.Background(), found.SessionID)
		sessionRepo.RevokeSession(context.Background(), found.SessionID)
	}

	// Verify: session should be gone
	_, err := sessionRepo.FindSession(context.Background(), sessionID)
	if err == nil {
		t.Error("session should have been revoked after theft detection")
	}

	// Verify: all tokens for that session should be revoked
	active := tokenRepo.activeTokensForSession(sessionID)
	if active != 0 {
		t.Errorf("expected 0 active tokens for session, got %d", active)
	}
}

// ============================================================================
// Tests: Max Sessions Enforcement
// ============================================================================

func TestMaxSessions_OldestEvicted(t *testing.T) {
	sessionRepo := newMockSessionRepo()
	tokenRepo := newMockTokenRepo()

	userID := kernel.NewUserID("user-1")
	tenantID := kernel.NewTenantID("tenant-1")
	maxSessions := 3

	// Create maxSessions sessions with staggered activity times
	for i := 0; i < maxSessions; i++ {
		sessionID := "session-" + string(rune('A'+i))
		sessionRepo.SaveSession(context.Background(), auth.UserSession{
			ID:           sessionID,
			UserID:       userID,
			TenantID:     tenantID,
			ExpiresAt:    time.Now().Add(24 * time.Hour),
			CreatedAt:    time.Now(),
			LastActivity: time.Now().Add(time.Duration(i) * time.Minute), // A is oldest
		})
		tokenRepo.SaveRefreshToken(context.Background(), auth.RefreshToken{
			ID:        "rt-" + sessionID,
			Token:     "token-" + sessionID,
			UserID:    userID,
			TenantID:  tenantID,
			SessionID: sessionID,
			ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
			CreatedAt: time.Now(),
		})
	}

	// Verify we're at max
	count, _ := sessionRepo.CountActiveSessions(context.Background(), userID)
	if count != maxSessions {
		t.Fatalf("expected %d sessions, got %d", maxSessions, count)
	}

	// Simulate enforcement: evict oldest if at limit
	if count >= maxSessions {
		oldest, err := sessionRepo.FindOldestSession(context.Background(), userID)
		if err != nil {
			t.Fatalf("FindOldestSession: %v", err)
		}

		if oldest.ID != "session-A" {
			t.Errorf("expected oldest session to be session-A, got %s", oldest.ID)
		}

		tokenRepo.RevokeRefreshTokensBySessionID(context.Background(), oldest.ID)
		sessionRepo.RevokeSession(context.Background(), oldest.ID)
	}

	// Now we should have maxSessions-1 sessions
	count, _ = sessionRepo.CountActiveSessions(context.Background(), userID)
	if count != maxSessions-1 {
		t.Errorf("expected %d sessions after eviction, got %d", maxSessions-1, count)
	}

	// session-A should be gone
	_, err := sessionRepo.FindSession(context.Background(), "session-A")
	if err == nil {
		t.Error("session-A should have been evicted")
	}

	// session-A tokens should be revoked
	active := tokenRepo.activeTokensForSession("session-A")
	if active != 0 {
		t.Errorf("expected 0 active tokens for evicted session, got %d", active)
	}
}

// ============================================================================
// Tests: Session-Token Linking
// ============================================================================

func TestSessionTokenLinking_RevokeSessionRevokesTokens(t *testing.T) {
	tokenRepo := newMockTokenRepo()
	sessionRepo := newMockSessionRepo()

	userID := kernel.NewUserID("user-1")
	tenantID := kernel.NewTenantID("tenant-1")
	sessionID := "session-device-1"

	// Save session
	sessionRepo.SaveSession(context.Background(), auth.UserSession{
		ID:           sessionID,
		UserID:       userID,
		TenantID:     tenantID,
		ExpiresAt:    time.Now().Add(24 * time.Hour),
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
	})

	// Save multiple tokens for this session (simulating rotations)
	for i := 0; i < 5; i++ {
		revoked := i < 4 // all but the latest are revoked (simulating rotation history)
		tokenRepo.SaveRefreshToken(context.Background(), auth.RefreshToken{
			ID:        "rt-" + string(rune('0'+i)),
			Token:     "token-" + string(rune('0'+i)),
			UserID:    userID,
			TenantID:  tenantID,
			SessionID: sessionID,
			ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
			CreatedAt: time.Now(),
			IsRevoked: revoked,
		})
	}

	// Active tokens before revocation
	activeBefore := tokenRepo.activeTokensForSession(sessionID)
	if activeBefore != 1 {
		t.Errorf("expected 1 active token before revocation, got %d", activeBefore)
	}

	// Revoke session → should revoke all its tokens
	tokenRepo.RevokeRefreshTokensBySessionID(context.Background(), sessionID)
	sessionRepo.RevokeSession(context.Background(), sessionID)

	// Verify all tokens are revoked
	activeAfter := tokenRepo.activeTokensForSession(sessionID)
	if activeAfter != 0 {
		t.Errorf("expected 0 active tokens after session revocation, got %d", activeAfter)
	}

	// Verify session is gone
	_, err := sessionRepo.FindSession(context.Background(), sessionID)
	if err == nil {
		t.Error("session should have been revoked")
	}
}

// ============================================================================
// Tests: Single vs All Logout
// ============================================================================

func TestSingleLogout_OnlyCurrentSession(t *testing.T) {
	tokenRepo := newMockTokenRepo()
	sessionRepo := newMockSessionRepo()

	userID := kernel.NewUserID("user-1")
	tenantID := kernel.NewTenantID("tenant-1")

	// Create two sessions (two devices)
	for _, id := range []string{"session-phone", "session-laptop"} {
		sessionRepo.SaveSession(context.Background(), auth.UserSession{
			ID:           id,
			UserID:       userID,
			TenantID:     tenantID,
			ExpiresAt:    time.Now().Add(24 * time.Hour),
			CreatedAt:    time.Now(),
			LastActivity: time.Now(),
		})
		tokenRepo.SaveRefreshToken(context.Background(), auth.RefreshToken{
			ID:        "rt-" + id,
			Token:     "token-" + id,
			UserID:    userID,
			TenantID:  tenantID,
			SessionID: id,
			ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
			CreatedAt: time.Now(),
		})
	}

	// Single logout: revoke only "session-phone"
	tokenRepo.RevokeRefreshTokensBySessionID(context.Background(), "session-phone")
	sessionRepo.RevokeSession(context.Background(), "session-phone")

	// session-phone should be gone
	_, err := sessionRepo.FindSession(context.Background(), "session-phone")
	if err == nil {
		t.Error("session-phone should be revoked")
	}
	if tokenRepo.activeTokensForSession("session-phone") != 0 {
		t.Error("session-phone tokens should be revoked")
	}

	// session-laptop should still be active
	laptop, err := sessionRepo.FindSession(context.Background(), "session-laptop")
	if err != nil {
		t.Fatalf("session-laptop should still exist: %v", err)
	}
	if laptop.IsExpired() {
		t.Error("session-laptop should not be expired")
	}
	if tokenRepo.activeTokensForSession("session-laptop") != 1 {
		t.Error("session-laptop should still have 1 active token")
	}
}

func TestLogoutAll_AllSessionsRevoked(t *testing.T) {
	tokenRepo := newMockTokenRepo()
	sessionRepo := newMockSessionRepo()

	userID := kernel.NewUserID("user-1")
	tenantID := kernel.NewTenantID("tenant-1")

	// Create three sessions
	for _, id := range []string{"session-1", "session-2", "session-3"} {
		sessionRepo.SaveSession(context.Background(), auth.UserSession{
			ID:           id,
			UserID:       userID,
			TenantID:     tenantID,
			ExpiresAt:    time.Now().Add(24 * time.Hour),
			CreatedAt:    time.Now(),
			LastActivity: time.Now(),
		})
		tokenRepo.SaveRefreshToken(context.Background(), auth.RefreshToken{
			ID:        "rt-" + id,
			Token:     "token-" + id,
			UserID:    userID,
			TenantID:  tenantID,
			SessionID: id,
			ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
			CreatedAt: time.Now(),
		})
	}

	// Logout all
	tokenRepo.RevokeAllUserTokens(context.Background(), userID)
	sessionRepo.RevokeAllUserSessions(context.Background(), userID)

	// All sessions should be gone
	sessions, _ := sessionRepo.FindUserSessions(context.Background(), userID)
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions after logout all, got %d", len(sessions))
	}

	// All tokens should be revoked
	for _, id := range []string{"session-1", "session-2", "session-3"} {
		if tokenRepo.activeTokensForSession(id) != 0 {
			t.Errorf("expected 0 active tokens for %s after logout all", id)
		}
	}
}
