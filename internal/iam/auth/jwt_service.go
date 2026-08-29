package auth

import (
	"fmt"
	"time"

	"github.com/Abraxas-365/manifesto/internal/config"
	"github.com/Abraxas-365/manifesto/internal/kernel"
	"github.com/golang-jwt/jwt/v5"
)

// JWTService is a JWT-based implementation of TokenService
type JWTService struct {
	secretKey       []byte
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
	issuer          string
	audience        []string
}

// NewJWTServiceFromConfig creates a new JWT service instance from config
func NewJWTServiceFromConfig(cfg *config.JWTConfig) *JWTService {
	return &JWTService{
		secretKey:       []byte(cfg.SecretKey),
		accessTokenTTL:  cfg.AccessTokenTTL,
		refreshTokenTTL: cfg.RefreshTokenTTL,
		issuer:          cfg.Issuer,
		audience:        cfg.Audience,
	}
}

// JWTClaims holds custom JWT claims
type JWTClaims struct {
	UserID    kernel.UserID   `json:"user_id"`
	TenantID  kernel.TenantID `json:"tenant_id"`
	SessionID string          `json:"session_id"`
	Email     string          `json:"email"`
	Name      string          `json:"name"`
	Scopes   []string        `json:"scopes"`
	jwt.RegisteredClaims
}

// GenerateAccessToken generates a JWT access token
func (j *JWTService) GenerateAccessToken(userID kernel.UserID, tenantID kernel.TenantID, claims map[string]any) (string, error) {
	now := time.Now()

	// Extract additional claims
	email, _ := claims["email"].(string)
	name, _ := claims["name"].(string)
	scopes, _ := claims["scopes"].([]string)
	sessionID, _ := claims["session_id"].(string)

	// Default to empty scopes if not provided
	if scopes == nil {
		scopes = []string{}
	}

	jwtClaims := JWTClaims{
		UserID:    userID,
		TenantID:  tenantID,
		SessionID: sessionID,
		Email:     email,
		Name:      name,
		Scopes:    scopes,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    j.issuer,
			Subject:   userID.String(),
			Audience:  j.audience,
			ExpiresAt: jwt.NewNumericDate(now.Add(j.accessTokenTTL)),
			NotBefore: jwt.NewNumericDate(now),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwtClaims)

	tokenString, err := token.SignedString(j.secretKey)
	if err != nil {
		return "", ErrTokenGenerationFailed().WithDetail("error", err.Error())
	}

	return tokenString, nil
}

// ValidateAccessToken validates and decodes an access token
func (j *JWTService) ValidateAccessToken(tokenString string) (*TokenClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (any, error) {
		// Verify the signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return j.secretKey, nil
	})

	if err != nil {
		return nil, ErrTokenValidationFailed().WithDetail("error", err.Error())
	}

	if !token.Valid {
		return nil, ErrTokenValidationFailed().WithDetail("error", "token is invalid")
	}

	jwtClaims, ok := token.Claims.(*JWTClaims)
	if !ok {
		return nil, ErrTokenValidationFailed().WithDetail("error", "invalid claims type")
	}

	return &TokenClaims{
		UserID:    jwtClaims.UserID,
		TenantID:  jwtClaims.TenantID,
		SessionID: jwtClaims.SessionID,
		Email:     jwtClaims.Email,
		Name:      jwtClaims.Name,
		Scopes:    jwtClaims.Scopes,
		IssuedAt:  jwtClaims.IssuedAt.Time,
		ExpiresAt: jwtClaims.ExpiresAt.Time,
	}, nil
}

// GenerateRefreshToken generates a simple refresh token
func (j *JWTService) GenerateRefreshToken(userID kernel.UserID) (string, error) {
	now := time.Now()

	claims := jwt.RegisteredClaims{
		Issuer:    j.issuer,
		Subject:   userID.String(),
		Audience:  j.audience,
		ExpiresAt: jwt.NewNumericDate(now.Add(j.refreshTokenTTL)),
		NotBefore: jwt.NewNumericDate(now),
		IssuedAt:  jwt.NewNumericDate(now),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	tokenString, err := token.SignedString(j.secretKey)
	if err != nil {
		return "", ErrTokenGenerationFailed().WithDetail("error", err.Error())
	}

	return tokenString, nil
}
