package kernel

// Page represents pagination metadata
type Page struct {
	Number int `json:"page"`      // Current page number (1-based)
	Size   int `json:"page_size"` // Number of records per page
	Total  int `json:"total"`     // Total number of records
	Pages  int `json:"pages"`     // Total number of pages
}

// Paginated is a generic container for paginated data with metadata
type Paginated[T any] struct {
	Items []T  `json:"items"`      // The paginated items
	Page  Page `json:"pagination"` // Pagination metadata
	Empty bool `json:"empty"`      // Whether the result contains any items
}

// NewPaginated creates a new paginated result with calculated fields
func NewPaginated[T any](items []T, page, size, total int) Paginated[T] {
	pages := 0
	if size > 0 {
		pages = (total + size - 1) / size // Ceiling division
	}

	return Paginated[T]{
		Items: items,
		Page: Page{
			Number: page,
			Size:   size,
			Total:  total,
			Pages:  pages,
		},
		Empty: len(items) == 0,
	}
}

// HasNext returns whether there are more pages after the current one
func (p Paginated[T]) HasNext() bool {
	return p.Page.Number < p.Page.Pages
}

// HasPrevious returns whether there are pages before the current one
func (p Paginated[T]) HasPrevious() bool {
	return p.Page.Number > 1
}

// PaginationOptions holds options for pagination queries
type PaginationOptions struct {
	Page     int // Page number (1-based)
	PageSize int // Number of records per page
}
