package authinfra

import (
	"context"
	"log"
	"time"

	"github.com/Abraxas-365/manifesto/internal/iam/auth"
)

// CleanupService is the background cleanup service
type CleanupService struct {
	tokenRepo         auth.TokenRepository
	sessionRepo       auth.SessionRepository
	passwordResetRepo auth.PasswordResetRepository
	interval          time.Duration
}

// NewCleanupService creates a new cleanup service
func NewCleanupService(
	tokenRepo auth.TokenRepository,
	sessionRepo auth.SessionRepository,
	passwordResetRepo auth.PasswordResetRepository,
	interval time.Duration,
) *CleanupService {
	return &CleanupService{
		tokenRepo:         tokenRepo,
		sessionRepo:       sessionRepo,
		passwordResetRepo: passwordResetRepo,
		interval:          interval,
	}
}

// Start starts the cleanup service
func (s *CleanupService) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Run initial cleanup
	s.runCleanup(ctx)

	for {
		select {
		case <-ctx.Done():
			log.Println("Cleanup service stopped")
			return
		case <-ticker.C:
			s.runCleanup(ctx)
		}
	}
}

// runCleanup executes the cleanup tasks
func (s *CleanupService) runCleanup(ctx context.Context) {
	log.Println("Running cleanup tasks...")

	// Clean expired refresh tokens
	if err := s.tokenRepo.CleanExpiredTokens(ctx); err != nil {
		log.Printf("Error cleaning expired tokens: %v", err)
	}

	// Clean expired sessions
	if err := s.sessionRepo.CleanExpiredSessions(ctx); err != nil {
		log.Printf("Error cleaning expired sessions: %v", err)
	}

	// Clean expired reset tokens
	if err := s.passwordResetRepo.CleanExpiredResetTokens(ctx); err != nil {
		log.Printf("Error cleaning expired reset tokens: %v", err)
	}

	log.Println("Cleanup tasks completed")
}
