package iam

import (
	"net/http"

	"github.com/Abraxas-365/manifesto/pkg/errx"
)

// ============================================================================
// Error Registry
// ============================================================================

var ErrRegistry = errx.NewRegistry("IAM")

var (
	CodeUnauthorized = ErrRegistry.Register("UNAUTHORIZED", errx.TypeAuthorization, http.StatusUnauthorized, "Unauthorized")
	CodeInvalidToken = ErrRegistry.Register("INVALID_TOKEN", errx.TypeAuthorization, http.StatusUnauthorized, "Invalid or expired token")
	CodeAccessDenied = ErrRegistry.Register("ACCESS_DENIED", errx.TypeAuthorization, http.StatusForbidden, "Access denied")
)

// Helper functions
func ErrUnauthorized() *errx.Error {
	return ErrRegistry.New(CodeUnauthorized)
}

func ErrInvalidToken() *errx.Error {
	return ErrRegistry.New(CodeInvalidToken)
}

func ErrAccessDenied() *errx.Error {
	return ErrRegistry.New(CodeAccessDenied)
}

// OAuthProvider represents supported OAuth providers
type OAuthProvider string

const (
	OAuthProviderGoogle    OAuthProvider = "GOOGLE"
	OAuthProviderMicrosoft OAuthProvider = "MICROSOFT"
	OAuthProviderAuth0     OAuthProvider = "AUTH0"
)

// GetProviderName returns the human-readable provider name
func (p OAuthProvider) GetProviderName() string {
	switch p {
	case OAuthProviderGoogle:
		return "Google"
	case OAuthProviderMicrosoft:
		return "Microsoft"
	case OAuthProviderAuth0:
		return "Auth0"
	default:
		return "Unknown"
	}
}
