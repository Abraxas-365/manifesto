// pkg/iam/otp/otpsrv/service.go

package otpsrv

import (
	"context"
	"time"

	"github.com/Abraxas-365/manifesto/pkg/config"
	"github.com/Abraxas-365/manifesto/pkg/errx"
	"github.com/Abraxas-365/manifesto/pkg/iam/otp"
	"github.com/google/uuid"
)

type OTPService struct {
	repo                otp.Repository
	notificationService otp.NotificationService
	config              *config.OTPConfig
}

func NewOTPService(
	repo otp.Repository,
	notificationService otp.NotificationService,
	cfg *config.OTPConfig,
) *OTPService {
	return &OTPService{
		repo:                repo,
		notificationService: notificationService,
		config:              cfg,
	}
}

// GenerateOTP creates and sends an OTP
func (s *OTPService) GenerateOTP(ctx context.Context, contact string, purpose otp.OTPPurpose) (*otp.OTP, error) {
	// Rate limiting check
	existing, _ := s.repo.GetLatestByContact(ctx, contact, purpose)
	if existing != nil && existing.IsValid() {
		timeSinceCreation := time.Since(existing.CreatedAt)
		if timeSinceCreation < s.config.RateLimitWindow {
			return nil, otp.ErrTooManyRequests().WithDetail(
				"retry_after",
				s.config.RateLimitWindow.String(),
			)
		}
	}

	// Generate code with configurable length
	code, err := otp.GenerateOTPCode(s.config.CodeLength)
	if err != nil {
		return nil, errx.Wrap(err, "failed to generate OTP code", errx.TypeInternal)
	}

	// Create OTP
	newOTP := &otp.OTP{
		ID:          uuid.NewString(),
		Contact:     contact,
		Code:        code,
		Purpose:     purpose,
		ExpiresAt:   time.Now().Add(s.config.ExpirationTime),
		Attempts:    0,
		MaxAttempts: s.config.MaxAttempts,
		CreatedAt:   time.Now(),
	}

	// Save OTP
	if err := s.repo.Create(ctx, newOTP); err != nil {
		return nil, errx.Wrap(err, "failed to save OTP", errx.TypeInternal)
	}

	// Send notification
	if err := s.notificationService.SendOTP(ctx, contact, code); err != nil {
		return nil, errx.Wrap(err, "failed to send OTP", errx.TypeExternal)
	}

	return newOTP, nil
}

// VerifyOTP validates an OTP code
func (s *OTPService) VerifyOTP(ctx context.Context, contact string, code string) (*otp.OTP, error) {
	otpEntity, err := s.repo.GetByContactAndCode(ctx, contact, code)
	if err != nil {
		return nil, otp.ErrInvalidOTP()
	}

	if otpEntity.IsExpired() {
		return nil, otp.ErrOTPExpired()
	}

	if otpEntity.Attempts >= otpEntity.MaxAttempts {
		return nil, otp.ErrTooManyAttempts()
	}

	if otpEntity.VerifiedAt != nil {
		return nil, otp.ErrOTPAlreadyUsed()
	}

	otpEntity.IncrementAttempts()

	if otpEntity.Code == code {
		if err := otpEntity.Verify(); err != nil {
			return nil, err
		}

		if err := s.repo.Update(ctx, otpEntity); err != nil {
			return nil, errx.Wrap(err, "failed to update OTP", errx.TypeInternal)
		}

		return otpEntity, nil
	}

	if err := s.repo.Update(ctx, otpEntity); err != nil {
		return nil, errx.Wrap(err, "failed to update OTP attempts", errx.TypeInternal)
	}

	remainingAttempts := otpEntity.MaxAttempts - otpEntity.Attempts
	return nil, otp.ErrInvalidOTP().WithDetail("attempts_remaining", remainingAttempts)
}
