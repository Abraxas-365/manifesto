package otp

import (
	"github.com/Abraxas-365/manifesto/pkg/errx"
	"net/http"
)

var ErrRegistry = errx.NewRegistry("OTP")

var (
	CodeInvalidOTP      = ErrRegistry.Register("INVALID_OTP", errx.TypeValidation, http.StatusBadRequest, "Invalid or incorrect OTP code")
	CodeOTPExpired      = ErrRegistry.Register("OTP_EXPIRED", errx.TypeValidation, http.StatusBadRequest, "OTP code has expired")
	CodeOTPAlreadyUsed  = ErrRegistry.Register("OTP_ALREADY_USED", errx.TypeBusiness, http.StatusBadRequest, "OTP code has already been used")
	CodeTooManyAttempts = ErrRegistry.Register("TOO_MANY_ATTEMPTS", errx.TypeBusiness, http.StatusTooManyRequests, "Too many verification attempts")
	CodeTooManyRequests = ErrRegistry.Register("TOO_MANY_REQUESTS", errx.TypeBusiness, http.StatusTooManyRequests, "Too many OTP requests")
)

func ErrInvalidOTP() *errx.Error      { return ErrRegistry.New(CodeInvalidOTP) }
func ErrOTPExpired() *errx.Error      { return ErrRegistry.New(CodeOTPExpired) }
func ErrOTPAlreadyUsed() *errx.Error  { return ErrRegistry.New(CodeOTPAlreadyUsed) }
func ErrTooManyAttempts() *errx.Error { return ErrRegistry.New(CodeTooManyAttempts) }
func ErrTooManyRequests() *errx.Error { return ErrRegistry.New(CodeTooManyRequests) }
