package authinfra

import (
	"context"
	"time"

	"github.com/Abraxas-365/manifesto/pkg/kernel"
	"github.com/Abraxas-365/manifesto/pkg/logx"
)

// LogxAuditService implements auth.AuditService using structured logx logging.
type LogxAuditService struct{}

func NewLogxAuditService() *LogxAuditService {
	return &LogxAuditService{}
}

func (s *LogxAuditService) LogLoginAttempt(_ context.Context, userID kernel.UserID, tenantID kernel.TenantID, method string, success bool, ip string, userAgent string) {
	logx.WithFields(logx.Fields{
		"audit_event": "login_attempt",
		"user_id":     userID,
		"tenant_id":   tenantID,
		"method":      method,
		"success":     success,
		"ip":          ip,
		"user_agent":  userAgent,
		"timestamp":   time.Now(),
	}).Info("Audit: login attempt")
}

func (s *LogxAuditService) LogLogout(_ context.Context, userID kernel.UserID, tenantID kernel.TenantID, ip string) {
	logx.WithFields(logx.Fields{
		"audit_event": "logout",
		"user_id":     userID,
		"tenant_id":   tenantID,
		"ip":          ip,
		"timestamp":   time.Now(),
	}).Info("Audit: logout")
}

func (s *LogxAuditService) LogTokenRefresh(_ context.Context, userID kernel.UserID, tenantID kernel.TenantID, ip string) {
	logx.WithFields(logx.Fields{
		"audit_event": "token_refresh",
		"user_id":     userID,
		"tenant_id":   tenantID,
		"ip":          ip,
		"timestamp":   time.Now(),
	}).Info("Audit: token refresh")
}

func (s *LogxAuditService) LogOTPVerification(_ context.Context, contact string, success bool, ip string) {
	logx.WithFields(logx.Fields{
		"audit_event": "otp_verification",
		"contact":     contact,
		"success":     success,
		"ip":          ip,
		"timestamp":   time.Now(),
	}).Info("Audit: OTP verification")
}

func (s *LogxAuditService) LogAccountCreated(_ context.Context, userID kernel.UserID, tenantID kernel.TenantID, method string, ip string) {
	logx.WithFields(logx.Fields{
		"audit_event": "account_created",
		"user_id":     userID,
		"tenant_id":   tenantID,
		"method":      method,
		"ip":          ip,
		"timestamp":   time.Now(),
	}).Info("Audit: account created")
}

func (s *LogxAuditService) LogAccountLinked(_ context.Context, userID kernel.UserID, tenantID kernel.TenantID, method string, ip string) {
	logx.WithFields(logx.Fields{
		"audit_event": "account_linked",
		"user_id":     userID,
		"tenant_id":   tenantID,
		"method":      method,
		"ip":          ip,
		"timestamp":   time.Now(),
	}).Info("Audit: account linked")
}
