package otp

import "context"

type Repository interface {
	Create(ctx context.Context, otp *OTP) error
	GetByContactAndCode(ctx context.Context, contact string, code string) (*OTP, error)
	GetLatestByContact(ctx context.Context, contact string, purpose OTPPurpose) (*OTP, error)
	Update(ctx context.Context, otp *OTP) error
	DeleteExpired(ctx context.Context) error
}

// NotificationService is a generic interface for sending OTP codes
type NotificationService interface {
	SendOTP(ctx context.Context, contact string, code string) error
}
