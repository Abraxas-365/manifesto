package scopes

// Domain-specific scopes — add project-specific scopes here.
// They are merged into the global scope registry at init time.

// DomainScopeCategories organizes domain-specific scopes by category.
var DomainScopeCategories = map[string][]string{}

// DomainScopeDescriptions provides descriptions for domain scopes.
var DomainScopeDescriptions = map[string]string{}
