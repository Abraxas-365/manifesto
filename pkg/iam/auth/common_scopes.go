// pkg/iam/auth/scopes_common.go
package auth

// ============================================================================
// COMMON SCOPES - Reusable across any project
// ============================================================================

const (
	// Super scope - full access to everything
	ScopeAll = "*"

	// Admin scopes
	ScopeAdminAll   = "admin:*"
	ScopeAdminRead  = "admin:read"
	ScopeAdminWrite = "admin:write"

	// User management scopes
	ScopeUsersAll    = "users:*"
	ScopeUsersRead   = "users:read"
	ScopeUsersWrite  = "users:write"
	ScopeUsersDelete = "users:delete"
	ScopeUsersInvite = "users:invite"

	// Role management scopes
	ScopeRolesAll    = "roles:*"
	ScopeRolesRead   = "roles:read"
	ScopeRolesWrite  = "roles:write"
	ScopeRolesDelete = "roles:delete"
	ScopeRolesAssign = "roles:assign"

	// Tenant management scopes
	ScopeTenantsAll    = "tenants:*"
	ScopeTenantsRead   = "tenants:read"
	ScopeTenantsWrite  = "tenants:write"
	ScopeTenantsDelete = "tenants:delete"
	ScopeTenantsConfig = "tenants:config"

	// API Key scopes
	ScopeAPIKeysAll    = "api_keys:*"
	ScopeAPIKeysRead   = "api_keys:read"
	ScopeAPIKeysWrite  = "api_keys:write"
	ScopeAPIKeysDelete = "api_keys:delete"
	ScopeAPIKeysRevoke = "api_keys:revoke"

	// Settings scopes
	ScopeSettingsAll   = "settings:*"
	ScopeSettingsRead  = "settings:read"
	ScopeSettingsWrite = "settings:write"

	// Audit log scopes
	ScopeAuditAll  = "audit:*"
	ScopeAuditRead = "audit:read"

	// Reports/Analytics scopes (generic)
	ScopeReportsAll         = "reports:*"
	ScopeReportsView        = "reports:view"
	ScopeReportsExport      = "reports:export"
	ScopeReportsCreate      = "reports:create"
	ScopeAnalyticsDashboard = "analytics:dashboard"

	// Integration scopes (generic for any external integrations)
	ScopeIntegrationsAll    = "integrations:*"
	ScopeIntegrationsRead   = "integrations:read"
	ScopeIntegrationsWrite  = "integrations:write"
	ScopeIntegrationsDelete = "integrations:delete"
	ScopeIntegrationsTest   = "integrations:test"

	// Notification scopes
	ScopeNotificationsAll   = "notifications:*"
	ScopeNotificationsRead  = "notifications:read"
	ScopeNotificationsSend  = "notifications:send"
	ScopeNotificationsWrite = "notifications:write"

	// Template scopes (generic templates)
	ScopeTemplatesAll    = "templates:*"
	ScopeTemplatesRead   = "templates:read"
	ScopeTemplatesWrite  = "templates:write"
	ScopeTemplatesDelete = "templates:delete"
)

// CommonScopeCategories organizes common scopes by domain
var CommonScopeCategories = map[string][]string{
	"Administration": {
		ScopeAll,
		ScopeAdminAll,
		ScopeAdminRead,
		ScopeAdminWrite,
	},
	"Users": {
		ScopeUsersAll,
		ScopeUsersRead,
		ScopeUsersWrite,
		ScopeUsersDelete,
		ScopeUsersInvite,
	},
	"Roles": {
		ScopeRolesAll,
		ScopeRolesRead,
		ScopeRolesWrite,
		ScopeRolesDelete,
		ScopeRolesAssign,
	},
	"Tenants": {
		ScopeTenantsAll,
		ScopeTenantsRead,
		ScopeTenantsWrite,
		ScopeTenantsDelete,
		ScopeTenantsConfig,
	},
	"API Keys": {
		ScopeAPIKeysAll,
		ScopeAPIKeysRead,
		ScopeAPIKeysWrite,
		ScopeAPIKeysDelete,
		ScopeAPIKeysRevoke,
	},
	"Settings": {
		ScopeSettingsAll,
		ScopeSettingsRead,
		ScopeSettingsWrite,
	},
	"Audit": {
		ScopeAuditAll,
		ScopeAuditRead,
	},
	"Reports & Analytics": {
		ScopeReportsAll,
		ScopeReportsView,
		ScopeReportsExport,
		ScopeReportsCreate,
		ScopeAnalyticsDashboard,
	},
	"Integrations": {
		ScopeIntegrationsAll,
		ScopeIntegrationsRead,
		ScopeIntegrationsWrite,
		ScopeIntegrationsDelete,
		ScopeIntegrationsTest,
	},
	"Notifications": {
		ScopeNotificationsAll,
		ScopeNotificationsRead,
		ScopeNotificationsSend,
		ScopeNotificationsWrite,
	},
	"Templates": {
		ScopeTemplatesAll,
		ScopeTemplatesRead,
		ScopeTemplatesWrite,
		ScopeTemplatesDelete,
	},
}

// CommonScopeDescriptions provides human-readable descriptions
var CommonScopeDescriptions = map[string]string{
	// Super admin
	ScopeAll: "Full access to all system resources",

	// Admin
	ScopeAdminAll:   "Full administrative access",
	ScopeAdminRead:  "View administrative settings",
	ScopeAdminWrite: "Modify administrative settings",

	// Users
	ScopeUsersAll:    "Full access to user management",
	ScopeUsersRead:   "View users",
	ScopeUsersWrite:  "Create and edit users",
	ScopeUsersDelete: "Delete users",
	ScopeUsersInvite: "Invite new users",

	// Roles
	ScopeRolesAll:    "Full access to role management",
	ScopeRolesRead:   "View roles",
	ScopeRolesWrite:  "Create and edit roles",
	ScopeRolesDelete: "Delete roles",
	ScopeRolesAssign: "Assign roles to users",

	// Tenants
	ScopeTenantsAll:    "Full access to tenant management",
	ScopeTenantsRead:   "View tenants",
	ScopeTenantsWrite:  "Create and edit tenants",
	ScopeTenantsDelete: "Delete tenants",
	ScopeTenantsConfig: "Manage tenant configuration",

	// API Keys
	ScopeAPIKeysAll:    "Full access to API key management",
	ScopeAPIKeysRead:   "View API keys",
	ScopeAPIKeysWrite:  "Create and edit API keys",
	ScopeAPIKeysDelete: "Delete API keys",
	ScopeAPIKeysRevoke: "Revoke API keys",

	// Settings
	ScopeSettingsAll:   "Full access to settings",
	ScopeSettingsRead:  "View settings",
	ScopeSettingsWrite: "Modify settings",

	// Audit
	ScopeAuditAll:  "Full access to audit logs",
	ScopeAuditRead: "View audit logs",

	// Reports
	ScopeReportsAll:         "Full access to reporting",
	ScopeReportsView:        "View reports",
	ScopeReportsExport:      "Export reports",
	ScopeReportsCreate:      "Create custom reports",
	ScopeAnalyticsDashboard: "Access analytics dashboard",

	// Integrations
	ScopeIntegrationsAll:    "Full access to integrations",
	ScopeIntegrationsRead:   "View integrations",
	ScopeIntegrationsWrite:  "Create and edit integrations",
	ScopeIntegrationsDelete: "Delete integrations",
	ScopeIntegrationsTest:   "Test integrations",

	// Notifications
	ScopeNotificationsAll:   "Full access to notifications",
	ScopeNotificationsRead:  "View notifications",
	ScopeNotificationsSend:  "Send notifications",
	ScopeNotificationsWrite: "Configure notifications",

	// Templates
	ScopeTemplatesAll:    "Full access to templates",
	ScopeTemplatesRead:   "View templates",
	ScopeTemplatesWrite:  "Create and edit templates",
	ScopeTemplatesDelete: "Delete templates",
}

// CommonScopeGroups defines common role groupings
var CommonScopeGroups = map[string][]string{
	"super_admin": {
		ScopeAll,
	},
	"platform_admin": {
		ScopeAdminAll,
		ScopeUsersAll,
		ScopeRolesAll,
		ScopeTenantsAll,
		ScopeSettingsAll,
		ScopeAuditRead,
		ScopeAPIKeysAll,
	},
	"tenant_admin": {
		ScopeUsersAll,
		ScopeRolesAll,
		ScopeSettingsAll,
		ScopeAPIKeysAll,
		ScopeTenantsRead,
		ScopeTenantsConfig,
	},
	"user_manager": {
		ScopeUsersAll,
		ScopeRolesRead,
		ScopeRolesAssign,
		ScopeUsersInvite,
	},
	"analyst": {
		ScopeReportsAll,
		ScopeAnalyticsDashboard,
		ScopeAuditRead,
	},
	"api_admin": {
		ScopeAPIKeysAll,
		ScopeIntegrationsAll,
	},
	"settings_admin": {
		ScopeSettingsAll,
		ScopeTenantsConfig,
		ScopeTemplatesAll,
		ScopeNotificationsAll,
	},
	"auditor": {
		ScopeAuditRead,
		ScopeUsersRead,
		ScopeRolesRead,
		ScopeTenantsRead,
	},
	"viewer": {
		ScopeUsersRead,
		ScopeRolesRead,
		ScopeTenantsRead,
		ScopeReportsView,
	},
}
