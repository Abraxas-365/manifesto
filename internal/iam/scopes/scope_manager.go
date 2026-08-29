package scopes

import (
	"slices"
	"strings"
)

// platformPrefix is the reserved prefix for scopes that may only be held by
// users belonging to the platform (operator) tenant. Any scope string that
// starts with this prefix is considered a platform scope and will be rejected
// when a non-platform caller tries to assign it to a user, role, or API key.
const platformPrefix = "platform:"

// IsPlatformScope returns true if the scope is reserved for platform operators.
// Platform scopes use the "platform:" prefix and cannot be assigned by regular
// tenant admins — only callers who themselves hold a platform scope may grant them.
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

// CallerHasPlatformScope returns true if the caller's effective scopes include
// at least one platform scope (or the wildcard "*" super scope).
func CallerHasPlatformScope(callerScopes []string) bool {
	for _, s := range callerScopes {
		if s == ScopeAll || IsPlatformScope(s) {
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

// ValidateScope checks if a scope is valid.
func ValidateScope(scope string) bool {
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
