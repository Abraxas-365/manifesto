package iam

import (
	"net/http"

	"github.com/Abraxas-365/manifesto/pkg/errx"
)

// ============================================================================
// Error Registry - Registro de errores del módulo IAM
// ============================================================================

var ErrRegistry = errx.NewRegistry("IAM")

// Códigos de error del módulo IAM
var (
	// Errores comunes
	CodeUnauthorized = ErrRegistry.Register("UNAUTHORIZED", errx.TypeAuthorization, http.StatusUnauthorized, "No autorizado")
	CodeInvalidToken = ErrRegistry.Register("INVALID_TOKEN", errx.TypeAuthorization, http.StatusUnauthorized, "Token inválido o expirado")
	CodeAccessDenied = ErrRegistry.Register("ACCESS_DENIED", errx.TypeAuthorization, http.StatusForbidden, "Acceso denegado")
)

// Helper functions para crear errores comunes
func ErrUnauthorized() *errx.Error {
	return ErrRegistry.New(CodeUnauthorized)
}

func ErrInvalidToken() *errx.Error {
	return ErrRegistry.New(CodeInvalidToken)
}

func ErrAccessDenied() *errx.Error {
	return ErrRegistry.New(CodeAccessDenied)
}

// OAuthProvider representa los proveedores OAuth soportados
type OAuthProvider string

const (
	OAuthProviderGoogle    OAuthProvider = "GOOGLE"
	OAuthProviderMicrosoft OAuthProvider = "MICROSOFT"
	OAuthProviderAuth0     OAuthProvider = "AUTH0"
)

// GetProviderName retorna el nombre legible del proveedor
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
