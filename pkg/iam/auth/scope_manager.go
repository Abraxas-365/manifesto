package auth

import "slices"

import "maps"

import "strings"

// ScopeCategories combines common and domain-specific categories
var ScopeCategories map[string][]string

// ScopeDescriptions combines common and domain-specific descriptions
var ScopeDescriptions map[string]string

// ScopeGroups combines common and domain-specific groups
var ScopeGroups map[string][]string

func init() {
	// Merge categories
	ScopeCategories = make(map[string][]string)
	maps.Copy(ScopeCategories, CommonScopeCategories)
	maps.Copy(ScopeCategories, DomainScopeCategories)

	// Merge descriptions
	ScopeDescriptions = make(map[string]string)
	maps.Copy(ScopeDescriptions, CommonScopeDescriptions)
	maps.Copy(ScopeDescriptions, DomainScopeDescriptions)

	// Merge groups
	ScopeGroups = make(map[string][]string)
	maps.Copy(ScopeGroups, CommonScopeGroups)
	maps.Copy(ScopeGroups, DomainScopeGroups)
}

// GetScopesByGroup returns all scopes for a given group
func GetScopesByGroup(group string) []string {
	if scopes, exists := ScopeGroups[group]; exists {
		return scopes
	}
	return []string{}
}

// GetScopeDescription returns the description for a given scope
func GetScopeDescription(scope string) string {
	if desc, exists := ScopeDescriptions[scope]; exists {
		return desc
	}
	return "No description available"
}

// GetAllScopes returns all defined scopes
func GetAllScopes() []string {
	allScopes := []string{}
	for _, scopes := range ScopeCategories {
		allScopes = append(allScopes, scopes...)
	}
	return allScopes
}

// GetCommonScopes returns only common/reusable scopes
func GetCommonScopes() []string {
	allScopes := []string{}
	for _, scopes := range CommonScopeCategories {
		allScopes = append(allScopes, scopes...)
	}
	return allScopes
}

// GetDomainScopes returns only domain-specific scopes
func GetDomainScopes() []string {
	allScopes := []string{}
	for _, scopes := range DomainScopeCategories {
		allScopes = append(allScopes, scopes...)
	}
	return allScopes
}

// ValidateScope checks if a scope is valid
func ValidateScope(scope string) bool {
	if scope == ScopeAll {
		return true
	}

	for _, scopes := range ScopeCategories {
		if slices.Contains(scopes, scope) {
			return true
		}
	}
	return false
}

// IsCommonScope checks if a scope is a common/reusable scope
func IsCommonScope(scope string) bool {
	for _, scopes := range CommonScopeCategories {
		if slices.Contains(scopes, scope) {
			return true
		}
	}
	return false
}

// IsDomainScope checks if a scope is a domain-specific scope
func IsDomainScope(scope string) bool {
	for _, scopes := range DomainScopeCategories {
		if slices.Contains(scopes, scope) {
			return true
		}
	}
	return false
}

// GetScopeCategory returns the category of a scope
func GetScopeCategory(scope string) string {
	for category, scopes := range ScopeCategories {
		if slices.Contains(scopes, scope) {
			return category
		}
	}
	return "Unknown"
}

// ExpandWildcardScope expands a wildcard scope to all matching scopes
// e.g., "jobs:*" -> ["jobs:read", "jobs:write", "jobs:delete", ...]
func ExpandWildcardScope(wildcardScope string) []string {
	if wildcardScope == ScopeAll {
		return GetAllScopes()
	}

	if !strings.HasSuffix(wildcardScope, ":*") {
		return []string{wildcardScope}
	}

	prefix := strings.TrimSuffix(wildcardScope, ":*")
	expanded := []string{}

	for _, scopes := range ScopeCategories {
		for _, scope := range scopes {
			if strings.HasPrefix(scope, prefix+":") {
				expanded = append(expanded, scope)
			}
		}
	}

	return expanded
}
