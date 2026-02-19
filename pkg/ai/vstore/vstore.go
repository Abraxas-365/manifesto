package vstore

import (
	"context"
)

// ============================================================================
// LAYER 1: Core Capabilities (Single Responsibility Interfaces)
// ============================================================================

// VectorStorer is the minimal vector store interface - all providers must implement this
type VectorStorer interface {
	// Upsert inserts or updates vectors
	Upsert(ctx context.Context, vectors []Vector, opts ...Option) error

	// Query performs similarity search
	Query(ctx context.Context, vector []float32, opts ...Option) (*QueryResult, error)

	// Delete removes vectors by IDs
	Delete(ctx context.Context, ids []string, opts ...Option) error

	// Fetch retrieves vectors by IDs
	Fetch(ctx context.Context, ids []string, opts ...Option) ([]Vector, error)
}

// MetadataFilterer supports advanced metadata filtering
type MetadataFilterer interface {
	// QueryWithFilter performs filtered similarity search
	QueryWithFilter(ctx context.Context, vector []float32, filter Filter, opts ...Option) (*QueryResult, error)
}

// BatchProcessor supports efficient batch operations
type BatchProcessor interface {
	// UpsertBatch upserts vectors in optimized batches
	UpsertBatch(ctx context.Context, vectors []Vector, opts ...Option) (*BatchResult, error)

	// DeleteBatch deletes multiple vectors efficiently
	DeleteBatch(ctx context.Context, ids []string, opts ...Option) (*BatchResult, error)
}

// NamespaceManager supports namespace/partition management
type NamespaceManager interface {
	// ListNamespaces returns all namespaces
	ListNamespaces(ctx context.Context) ([]string, error)

	// CreateNamespace creates a new namespace
	CreateNamespace(ctx context.Context, namespace string) error

	// DeleteNamespace deletes a namespace and all its vectors
	DeleteNamespace(ctx context.Context, namespace string) error
}

// IndexManager supports index lifecycle management
type IndexManager interface {
	// CreateIndex creates a new vector index
	CreateIndex(ctx context.Context, config IndexConfig) error

	// DeleteIndex deletes an index
	DeleteIndex(ctx context.Context, indexName string) error

	// DescribeIndex returns index metadata
	DescribeIndex(ctx context.Context, indexName string) (*IndexInfo, error)

	// ListIndexes returns all indexes
	ListIndexes(ctx context.Context) ([]IndexInfo, error)
}

// HybridSearcher supports hybrid vector + text search
type HybridSearcher interface {
	// HybridQuery performs combined vector and keyword search
	HybridQuery(ctx context.Context, vector []float32, query string, opts ...Option) (*QueryResult, error)
}

// SparseVectorSupport supports sparse vector operations
type SparseVectorSupport interface {
	// UpsertSparse upserts sparse vectors
	UpsertSparse(ctx context.Context, vectors []SparseVector, opts ...Option) error

	// QuerySparse queries with sparse vectors
	QuerySparse(ctx context.Context, vector SparseVector, opts ...Option) (*QueryResult, error)
}

// StatisticsProvider provides index statistics
type StatisticsProvider interface {
	// GetStatistics returns vector store statistics
	GetStatistics(ctx context.Context, opts ...Option) (*Statistics, error)
}

// ============================================================================
// LAYER 2: Core Data Models
// ============================================================================

// Vector represents a dense vector with metadata
type Vector struct {
	// ID is the unique identifier
	ID string

	// Values is the dense vector
	Values []float32

	// Metadata is arbitrary key-value data
	Metadata map[string]any

	// SparseValues for hybrid dense/sparse vectors
	SparseValues *SparseVector
}

// SparseVector represents a sparse vector
type SparseVector struct {
	// Indices of non-zero values
	Indices []uint32

	// Values at those indices
	Values []float32
}

// QueryResult contains search results
type QueryResult struct {
	// Matches is the list of similar vectors
	Matches []Match

	// Namespace where results came from
	Namespace string

	// Usage statistics
	Usage Usage
}

// Match represents a single search result
type Match struct {
	// ID of the matched vector
	ID string

	// Score (similarity/distance)
	Score float32

	// Values of the vector (if requested)
	Values []float32

	// SparseValues (if applicable)
	SparseValues *SparseVector

	// Metadata associated with the vector
	Metadata map[string]any
}

// Filter represents metadata filtering
type Filter struct {
	// Must conditions (AND)
	Must []Condition

	// Should conditions (OR)
	Should []Condition

	// MustNot conditions (NOT)
	MustNot []Condition
}

// Condition represents a single filter condition
type Condition struct {
	// Field name in metadata
	Field string

	// Operator (eq, ne, gt, lt, gte, lte, in, nin, exists)
	Operator FilterOperator

	// Value to compare against
	Value any
}

// FilterOperator represents filter operations
type FilterOperator string

const (
	OpEqual              FilterOperator = "eq"
	OpNotEqual           FilterOperator = "ne"
	OpGreaterThan        FilterOperator = "gt"
	OpLessThan           FilterOperator = "lt"
	OpGreaterThanOrEqual FilterOperator = "gte"
	OpLessThanOrEqual    FilterOperator = "lte"
	OpIn                 FilterOperator = "in"
	OpNotIn              FilterOperator = "nin"
	OpExists             FilterOperator = "exists"
	OpContains           FilterOperator = "contains"
)

// IndexConfig represents index configuration
type IndexConfig struct {
	// Name of the index
	Name string

	// Dimension of vectors
	Dimension int

	// Metric for similarity calculation
	Metric Metric

	// Replicas for redundancy
	Replicas int

	// Shards for distribution
	Shards int

	// PodType (for providers that support it)
	PodType string

	// Metadata configuration
	MetadataConfig *MetadataConfig
}

// Metric represents distance/similarity metrics
type Metric string

const (
	MetricCosine     Metric = "cosine"
	MetricDotProduct Metric = "dotproduct"
	MetricEuclidean  Metric = "euclidean"
)

// MetadataConfig configures metadata indexing
type MetadataConfig struct {
	// Indexed fields for filtering
	Indexed []string
}

// IndexInfo contains index metadata
type IndexInfo struct {
	// Name of the index
	Name string

	// Dimension of vectors
	Dimension int

	// Metric used
	Metric Metric

	// Total vector count
	TotalVectorCount int64

	// Status (ready, initializing, etc.)
	Status string

	// Host endpoint
	Host string

	// Metadata
	Metadata map[string]any
}

// BatchResult contains batch operation results
type BatchResult struct {
	// SuccessCount is number of successful operations
	SuccessCount int

	// FailedCount is number of failed operations
	FailedCount int

	// Errors for failed operations
	Errors []BatchError
}

// BatchError represents an error in batch processing
type BatchError struct {
	// ID of the vector that failed
	ID string

	// Error message
	Error string
}

// Statistics represents vector store statistics
type Statistics struct {
	// Total vectors
	TotalVectorCount int64

	// Dimension
	Dimension int

	// Namespaces
	Namespaces []NamespaceStats

	// Index fullness (0-1)
	IndexFullness float32
}

// NamespaceStats represents namespace statistics
type NamespaceStats struct {
	// Name of the namespace
	Name string

	// Vector count
	VectorCount int64
}

// Usage represents API usage
type Usage struct {
	// ReadUnits consumed
	ReadUnits int

	// WriteUnits consumed
	WriteUnits int
}

// ============================================================================
// LAYER 3: Result Builder (optional, for complex results)
// ============================================================================

// QueryResultBuilder builds query results
type QueryResultBuilder struct {
	result QueryResult
}

func NewQueryResultBuilder() *QueryResultBuilder {
	return &QueryResultBuilder{
		result: QueryResult{
			Matches: make([]Match, 0),
		},
	}
}

func (b *QueryResultBuilder) WithMatches(matches []Match) *QueryResultBuilder {
	b.result.Matches = matches
	return b
}

func (b *QueryResultBuilder) WithNamespace(namespace string) *QueryResultBuilder {
	b.result.Namespace = namespace
	return b
}

func (b *QueryResultBuilder) WithUsage(usage Usage) *QueryResultBuilder {
	b.result.Usage = usage
	return b
}

func (b *QueryResultBuilder) Build() *QueryResult {
	return &b.result
}

// ============================================================================
// LAYER 4: Filter Builders (for convenience)
// ============================================================================

// NewFilter creates a new filter
func NewFilter() *Filter {
	return &Filter{
		Must:    make([]Condition, 0),
		Should:  make([]Condition, 0),
		MustNot: make([]Condition, 0),
	}
}

// Must adds a must condition
func (f *Filter) AddMust(field string, op FilterOperator, value any) *Filter {
	f.Must = append(f.Must, Condition{
		Field:    field,
		Operator: op,
		Value:    value,
	})
	return f
}

// Should adds a should condition
func (f *Filter) AddShould(field string, op FilterOperator, value any) *Filter {
	f.Should = append(f.Should, Condition{
		Field:    field,
		Operator: op,
		Value:    value,
	})
	return f
}

// MustNot adds a must not condition
func (f *Filter) AddMustNot(field string, op FilterOperator, value any) *Filter {
	f.MustNot = append(f.MustNot, Condition{
		Field:    field,
		Operator: op,
		Value:    value,
	})
	return f
}
