package scopes

import (
	"slices"
	"strings"
)

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
