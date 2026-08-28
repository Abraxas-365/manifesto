package scopes

const (
	// Super scope - full access to everything
	ScopeAll = "*"

	// User management scopes
	ScopeUsersAll    = "users:*"
	ScopeUsersRead   = "users:read"
	ScopeUsersWrite  = "users:write"
	ScopeUsersDelete = "users:delete"

	// Role management scopes
	ScopeRolesAll    = "roles:*"
	ScopeRolesRead   = "roles:read"
	ScopeRolesWrite  = "roles:write"
	ScopeRolesDelete = "roles:delete"
	ScopeRolesAssign = "roles:assign"

	// Scope management scopes
	ScopeScopesAll    = "scopes:*"
	ScopeScopesRead   = "scopes:read"
	ScopeScopesWrite  = "scopes:write"
	ScopeScopesAssign = "scopes:assign"

	// Tenant self-service scopes
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

	// Invitation scopes
	ScopeInvitationsAll    = "invitations:*"
	ScopeInvitationsRead   = "invitations:read"
	ScopeInvitationsWrite  = "invitations:write"
	ScopeInvitationsDelete = "invitations:delete"
	ScopeInvitationsRevoke = "invitations:revoke"

	// Platform admin scopes — only granted to users in the platform tenant.
	ScopePlatformTenantsAll     = "platform:tenants:*"
	ScopePlatformTenantsRead    = "platform:tenants:read"
	ScopePlatformTenantsWrite   = "platform:tenants:write"
	ScopePlatformTenantsDelete  = "platform:tenants:delete"
	ScopePlatformTenantsConfig  = "platform:tenants:config"
	ScopePlatformTenantsSuspend = "platform:tenants:suspend"
)

// ScopeCategories organizes all scopes by domain for the catalog endpoint.
var ScopeCategories = map[string][]string{
	"Users": {
		ScopeUsersAll,
		ScopeUsersRead,
		ScopeUsersWrite,
		ScopeUsersDelete,
	},
	"Roles": {
		ScopeRolesAll,
		ScopeRolesRead,
		ScopeRolesWrite,
		ScopeRolesDelete,
		ScopeRolesAssign,
	},
	"Scopes": {
		ScopeScopesAll,
		ScopeScopesRead,
		ScopeScopesWrite,
		ScopeScopesAssign,
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
	"Invitations": {
		ScopeInvitationsAll,
		ScopeInvitationsRead,
		ScopeInvitationsWrite,
		ScopeInvitationsDelete,
		ScopeInvitationsRevoke,
	},
	"Platform: Tenants": {
		ScopePlatformTenantsAll,
		ScopePlatformTenantsRead,
		ScopePlatformTenantsWrite,
		ScopePlatformTenantsDelete,
		ScopePlatformTenantsConfig,
		ScopePlatformTenantsSuspend,
	},
}

// ScopeDescriptions provides human-readable descriptions for the catalog endpoint.
var ScopeDescriptions = map[string]string{
	ScopeAll: "Full access to all system resources",

	// Users
	ScopeUsersAll:    "Full access to user management",
	ScopeUsersRead:   "View users",
	ScopeUsersWrite:  "Create and edit users",
	ScopeUsersDelete: "Delete users",

	// Roles
	ScopeRolesAll:    "Full access to role management",
	ScopeRolesRead:   "View roles",
	ScopeRolesWrite:  "Create and edit roles",
	ScopeRolesDelete: "Delete roles",
	ScopeRolesAssign: "Assign roles to users",

	// Scopes
	ScopeScopesAll:    "Full access to scope management",
	ScopeScopesRead:   "View available scopes and user scopes",
	ScopeScopesWrite:  "Set and modify user scopes",
	ScopeScopesAssign: "Add or remove scopes from users",

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

	// Invitations
	ScopeInvitationsAll:    "Full access to invitation management",
	ScopeInvitationsRead:   "View invitations",
	ScopeInvitationsWrite:  "Create invitations",
	ScopeInvitationsDelete: "Delete invitations",
	ScopeInvitationsRevoke: "Revoke invitations",

	// Platform admin
	ScopePlatformTenantsAll:     "Full access to platform tenant management",
	ScopePlatformTenantsRead:    "View all tenants across the platform",
	ScopePlatformTenantsWrite:   "Create and edit tenants",
	ScopePlatformTenantsDelete:  "Delete tenants",
	ScopePlatformTenantsConfig:  "Manage any tenant's configuration",
	ScopePlatformTenantsSuspend: "Suspend and activate tenants",
}
