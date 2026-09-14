package scopes

import (
	"slices"
	"strings"
)

// platformPrefix is reserved for the separate operator authority. Tenant IAM
// never grants these scopes, including to callers holding the tenant wildcard.
const platformPrefix = "platform:"

// IsPlatformScope reports whether a scope is outside tenant IAM.
func IsPlatformScope(scope string) bool {
	return strings.HasPrefix(scope, platformPrefix)
}

// ContainsPlatformScope returns true if any scope in the slice is a platform scope.
func ContainsPlatformScope(scopeList []string) bool {
	for _, s := range scopeList {
		if IsPlatformScope(s) {
			return true
		}
	}
	return false
}

// FilterNonPlatformCategories returns a copy of ScopeCategories with platform
// categories removed. Used by the scope catalog endpoint to hide platform
// scopes from non-platform callers.
func FilterNonPlatformCategories() map[string][]string {
	filtered := make(map[string][]string, len(ScopeCategories))
	for category, scopeList := range ScopeCategories {
		hasPlatform := false
		for _, s := range scopeList {
			if IsPlatformScope(s) {
				hasPlatform = true
				break
			}
		}
		if !hasPlatform {
			filtered[category] = scopeList
		}
	}
	return filtered
}

// GetScopeDescription returns the description for a given scope.
func GetScopeDescription(scope string) string {
	if desc, exists := ScopeDescriptions[scope]; exists {
		return desc
	}
	return "No description available"
}

// GetAllScopes returns all defined scopes.
func GetAllScopes() []string {
	var allScopes []string
	for _, s := range ScopeCategories {
		allScopes = append(allScopes, s...)
	}
	return allScopes
}

// GetNonPlatformScopes returns all scopes except platform-reserved ones.
func GetNonPlatformScopes() []string {
	var out []string
	for _, s := range ScopeCategories {
		for _, scope := range s {
			if !IsPlatformScope(scope) {
				out = append(out, scope)
			}
		}
	}
	return out
}

// ValidateScope checks if a scope is valid for tenant IAM.
func ValidateScope(scope string) bool {
	if IsPlatformScope(scope) {
		return false
	}
	if scope == ScopeAll {
		return true
	}
	for _, s := range ScopeCategories {
		if slices.Contains(s, scope) {
			return true
		}
	}
	return false
}

// GetScopeCategory returns the category of a scope.
func GetScopeCategory(scope string) string {
	for category, s := range ScopeCategories {
		if slices.Contains(s, scope) {
			return category
		}
	}
	return "Unknown"
}

// ExpandWildcardScope expands a wildcard scope to all matching scopes.
// e.g., "users:*" -> ["users:read", "users:write", "users:delete"]
func ExpandWildcardScope(wildcardScope string) []string {
	if wildcardScope == ScopeAll {
		return GetAllScopes()
	}

	if !strings.HasSuffix(wildcardScope, ":*") {
		return []string{wildcardScope}
	}

	prefix := strings.TrimSuffix(wildcardScope, ":*")
	var expanded []string

	for _, s := range ScopeCategories {
		for _, scope := range s {
			if strings.HasPrefix(scope, prefix+":") {
				expanded = append(expanded, scope)
			}
		}
	}

	return expanded
}
