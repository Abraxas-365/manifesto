package otp

import (
	"crypto/rand"
	"fmt"
	"time"
)

type OTPPurpose string

const (
	OTPPurposeJobApplication OTPPurpose = "JOB_APPLICATION"
	OTPPurposeVerification   OTPPurpose = "VERIFICATION"
)

type OTP struct {
	ID         string
	Contact    string // Email or phone
	Code       string
	Purpose    OTPPurpose
	ExpiresAt  time.Time
	VerifiedAt *time.Time
	Attempts   int
	CreatedAt  time.Time
}

func (o *OTP) IsValid() bool {
	return time.Now().Before(o.ExpiresAt) && o.VerifiedAt == nil && o.Attempts < 5
}

func (o *OTP) IsExpired() bool {
	return time.Now().After(o.ExpiresAt)
}

func (o *OTP) Verify() error {
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

func GenerateOTPCode() (string, error) {
	bytes := make([]byte, 3)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	code := fmt.Sprintf("%06d", int(bytes[0])<<16|int(bytes[1])<<8|int(bytes[2]))
	return code[:6], nil
}
