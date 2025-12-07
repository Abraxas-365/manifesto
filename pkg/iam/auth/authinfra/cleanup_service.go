package authinfra

import (
	"context"
	"log"
	"time"

	"github.com/Abraxas-365/manifesto/pkg/iam/auth"
)

// CleanupService servicio de limpieza en background
type CleanupService struct {
	tokenRepo         auth.TokenRepository
	sessionRepo       auth.SessionRepository
	passwordResetRepo auth.PasswordResetRepository
	interval          time.Duration
}

// NewCleanupService crea un nuevo servicio de limpieza
func NewCleanupService(
	tokenRepo auth.TokenRepository,
	sessionRepo auth.SessionRepository,
	passwordResetRepo auth.PasswordResetRepository,
) *CleanupService {
	return &CleanupService{
		tokenRepo:         tokenRepo,
		sessionRepo:       sessionRepo,
		passwordResetRepo: passwordResetRepo,
		interval:          1 * time.Hour, // Ejecutar cada hora
	}
}

// Start inicia el servicio de limpieza
func (s *CleanupService) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Ejecutar limpieza inicial
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

// runCleanup ejecuta las tareas de limpieza
func (s *CleanupService) runCleanup(ctx context.Context) {
	log.Println("Running cleanup tasks...")

	// Limpiar refresh tokens expirados
	if err := s.tokenRepo.CleanExpiredTokens(ctx); err != nil {
		log.Printf("Error cleaning expired tokens: %v", err)
	}

	// Limpiar sesiones expiradas
	if err := s.sessionRepo.CleanExpiredSessions(ctx); err != nil {
		log.Printf("Error cleaning expired sessions: %v", err)
	}

	// Limpiar tokens de reset expirados
	if err := s.passwordResetRepo.CleanExpiredResetTokens(ctx); err != nil {
		log.Printf("Error cleaning expired reset tokens: %v", err)
	}

	log.Println("Cleanup tasks completed")
}
