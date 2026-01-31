package otp

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"time"
)

type OTPPurpose string

const (
	OTPPurposeJobApplication OTPPurpose = "JOB_APPLICATION"
	OTPPurposeVerification   OTPPurpose = "VERIFICATION"
)

type OTP struct {
	ID          string
	Contact     string // Email or phone
	Code        string
	Purpose     OTPPurpose
	ExpiresAt   time.Time
	VerifiedAt  *time.Time
	Attempts    int
	CreatedAt   time.Time
	MaxAttempts int
}

func (o *OTP) IsValid() bool {
	return time.Now().Before(o.ExpiresAt) && o.VerifiedAt == nil && o.Attempts < o.MaxAttempts
}

func (o *OTP) IsExpired() bool {
	return time.Now().After(o.ExpiresAt)
}

func (o *OTP) Verify() error {
	if o.Attempts >= o.MaxAttempts {
		return ErrTooManyAttempts()
	}
	if o.IsExpired() {
		return ErrOTPExpired()
	}
	if o.Attempts >= 5 {
		return ErrTooManyAttempts()
	}
	now := time.Now()
	o.VerifiedAt = &now
	return nil
}

func (o *OTP) IncrementAttempts() {
	o.Attempts++
}

// GenerateOTPCode generates a cryptographically secure random OTP code
func GenerateOTPCode(length int) (string, error) {
	// Calculate max value (10^length - 1)
	max := new(big.Int)
	max.Exp(big.NewInt(10), big.NewInt(int64(length)), nil)

	// Generate random number between 0 and max-1
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}

	// Format with leading zeros
	format := fmt.Sprintf("%%0%dd", length)
	return fmt.Sprintf(format, n), nil
}
