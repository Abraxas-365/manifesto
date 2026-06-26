package authinfra

import (
	"github.com/Abraxas-365/manifesto/internal/iam/user"
	"golang.org/x/crypto/bcrypt"
)

// BcryptPasswordService is the bcrypt implementation of the password service
type BcryptPasswordService struct {
	cost int
}

// NewBcryptPasswordService creates a new password service instance
func NewBcryptPasswordService(cost int) user.PasswordService {
	if cost == 0 {
		cost = bcrypt.DefaultCost
	}
	return &BcryptPasswordService{
		cost: cost,
	}
}

// HashPassword hashes a password
func (s *BcryptPasswordService) HashPassword(password string) (string, error) {
	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(password), s.cost)
	if err != nil {
		return "", err
	}
	return string(hashedBytes), nil
}

// VerifyPassword verifies a password against its hash
func (s *BcryptPasswordService) VerifyPassword(hashedPassword, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
}
