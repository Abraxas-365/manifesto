package scopeapi

import (
	"github.com/Abraxas-365/manifesto/internal/iam"
	"github.com/Abraxas-365/manifesto/internal/iam/auth"
	"github.com/Abraxas-365/manifesto/internal/iam/scopes"
	"github.com/gofiber/fiber/v2"
)

// ScopeDetail provides detailed information about a scope for the catalog.
type ScopeDetail struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
}

// ScopeCatalogResponse is the response for the scope catalog endpoint.
type ScopeCatalogResponse struct {
	TotalScopes int                      `json:"total_scopes"`
	Categories  map[string][]ScopeDetail `json:"categories"`
}

// ScopeCatalogHandler serves the read-only scope catalog endpoint.
type ScopeCatalogHandler struct{}

func NewScopeCatalogHandler() *ScopeCatalogHandler {
	return &ScopeCatalogHandler{}
}

func (h *ScopeCatalogHandler) RegisterRoutes(router fiber.Router, authMiddleware *auth.UnifiedAuthMiddleware) {
	scopeRoutes := router.Group("/scopes", authMiddleware.Authenticate())
	scopeRoutes.Get("/", authMiddleware.RequireScope(scopes.ScopeScopesRead), h.GetCatalog)
}

// GetCatalog returns all scopes grouped by category.
// Platform scopes are hidden from non-platform callers.
func (h *ScopeCatalogHandler) GetCatalog(c *fiber.Ctx) error {
	authContext, ok := auth.GetAuthContext(c)
	if !ok {
		return iam.ErrUnauthorized()
	}

	var categories map[string][]string
	if scopes.CallerHasPlatformScope(authContext.Scopes) {
		categories = scopes.ScopeCategories
	} else {
		categories = scopes.FilterNonPlatformCategories()
	}

	result := make(map[string][]ScopeDetail)
	total := 0
	for category, scopeList := range categories {
		for _, scope := range scopeList {
			result[category] = append(result[category], ScopeDetail{
				Name:        scope,
				Description: scopes.GetScopeDescription(scope),
				Category:    category,
			})
			total++
		}
	}

	return c.JSON(ScopeCatalogResponse{
		TotalScopes: total,
		Categories:  result,
	})
}
