package vstore

import (
	"context"
	"fmt"
)

// Client provides unified access to vector store capabilities
type Client struct {
	storer VectorStorer

	// Optional capabilities
	metadataFilterer MetadataFilterer
	batchProcessor   BatchProcessor
	namespaceManager NamespaceManager
	indexManager     IndexManager
	hybridSearcher   HybridSearcher
	sparseSupport    SparseVectorSupport
	statsProvider    StatisticsProvider
}

// NewClient creates a client from a provider
func NewClient(storer VectorStorer) *Client {
	client := &Client{
		storer: storer,
	}

	// Detect all capabilities via type assertions
	if mf, ok := storer.(MetadataFilterer); ok {
		client.metadataFilterer = mf
	}
	if bp, ok := storer.(BatchProcessor); ok {
		client.batchProcessor = bp
	}
	if nm, ok := storer.(NamespaceManager); ok {
		client.namespaceManager = nm
	}
	if im, ok := storer.(IndexManager); ok {
		client.indexManager = im
	}
	if hs, ok := storer.(HybridSearcher); ok {
		client.hybridSearcher = hs
	}
	if ss, ok := storer.(SparseVectorSupport); ok {
		client.sparseSupport = ss
	}
	if sp, ok := storer.(StatisticsProvider); ok {
		client.statsProvider = sp
	}

	return client
}

// ============================================================================
// Core Operations
// ============================================================================

// Upsert inserts or updates vectors
func (c *Client) Upsert(ctx context.Context, vectors []Vector, opts ...Option) error {
	return c.storer.Upsert(ctx, vectors, opts...)
}

// Query performs similarity search
func (c *Client) Query(ctx context.Context, vector []float32, opts ...Option) (*QueryResult, error) {
	options := ApplyOptions(opts...)

	// Use filtered query if filter is provided and supported
	if options.Filter != nil && c.metadataFilterer != nil {
		return c.metadataFilterer.QueryWithFilter(ctx, vector, *options.Filter, opts...)
	}

	return c.storer.Query(ctx, vector, opts...)
}

// Delete removes vectors by IDs
func (c *Client) Delete(ctx context.Context, ids []string, opts ...Option) error {
	return c.storer.Delete(ctx, ids, opts...)
}

// Fetch retrieves vectors by IDs
func (c *Client) Fetch(ctx context.Context, ids []string, opts ...Option) ([]Vector, error) {
	return c.storer.Fetch(ctx, ids, opts...)
}

// ============================================================================
// Batch Operations
// ============================================================================

// UpsertBatch upserts vectors in optimized batches
func (c *Client) UpsertBatch(ctx context.Context, vectors []Vector, opts ...Option) (*BatchResult, error) {
	if c.batchProcessor != nil {
		return c.batchProcessor.UpsertBatch(ctx, vectors, opts...)
	}

	// Fallback to sequential upsert
	options := ApplyOptions(opts...)
	batchSize := options.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	result := &BatchResult{}
	for i := 0; i < len(vectors); i += batchSize {
		end := i + batchSize
		if end > len(vectors) {
			end = len(vectors)
		}

		batch := vectors[i:end]
		if err := c.storer.Upsert(ctx, batch, opts...); err != nil {
			result.FailedCount += len(batch)
			for _, v := range batch {
				result.Errors = append(result.Errors, BatchError{
					ID:    v.ID,
					Error: err.Error(),
				})
			}
		} else {
			result.SuccessCount += len(batch)
		}
	}

	return result, nil
}

// DeleteBatch deletes multiple vectors efficiently
func (c *Client) DeleteBatch(ctx context.Context, ids []string, opts ...Option) (*BatchResult, error) {
	if c.batchProcessor != nil {
		return c.batchProcessor.DeleteBatch(ctx, ids, opts...)
	}

	// Fallback to single delete
	if err := c.storer.Delete(ctx, ids, opts...); err != nil {
		return &BatchResult{
			FailedCount: len(ids),
		}, err
	}

	return &BatchResult{
		SuccessCount: len(ids),
	}, nil
}

// ============================================================================
// Namespace Operations
// ============================================================================

// ListNamespaces returns all namespaces
func (c *Client) ListNamespaces(ctx context.Context) ([]string, error) {
	if c.namespaceManager == nil {
		return nil, fmt.Errorf("namespace management not supported by this provider")
	}
	return c.namespaceManager.ListNamespaces(ctx)
}

// CreateNamespace creates a new namespace
func (c *Client) CreateNamespace(ctx context.Context, namespace string) error {
	if c.namespaceManager == nil {
		return fmt.Errorf("namespace management not supported by this provider")
	}
	return c.namespaceManager.CreateNamespace(ctx, namespace)
}

// DeleteNamespace deletes a namespace and all its vectors
func (c *Client) DeleteNamespace(ctx context.Context, namespace string) error {
	if c.namespaceManager == nil {
		return fmt.Errorf("namespace management not supported by this provider")
	}
	return c.namespaceManager.DeleteNamespace(ctx, namespace)
}

// ============================================================================
// Index Operations
// ============================================================================

// CreateIndex creates a new vector index
func (c *Client) CreateIndex(ctx context.Context, config IndexConfig) error {
	if c.indexManager == nil {
		return fmt.Errorf("index management not supported by this provider")
	}
	return c.indexManager.CreateIndex(ctx, config)
}

// DeleteIndex deletes an index
func (c *Client) DeleteIndex(ctx context.Context, indexName string) error {
	if c.indexManager == nil {
		return fmt.Errorf("index management not supported by this provider")
	}
	return c.indexManager.DeleteIndex(ctx, indexName)
}

// DescribeIndex returns index metadata
func (c *Client) DescribeIndex(ctx context.Context, indexName string) (*IndexInfo, error) {
	if c.indexManager == nil {
		return nil, fmt.Errorf("index management not supported by this provider")
	}
	return c.indexManager.DescribeIndex(ctx, indexName)
}

// ListIndexes returns all indexes
func (c *Client) ListIndexes(ctx context.Context) ([]IndexInfo, error) {
	if c.indexManager == nil {
		return nil, fmt.Errorf("index management not supported by this provider")
	}
	return c.indexManager.ListIndexes(ctx)
}

// ============================================================================
// Advanced Search Operations
// ============================================================================

// HybridQuery performs combined vector and keyword search
func (c *Client) HybridQuery(ctx context.Context, vector []float32, query string, opts ...Option) (*QueryResult, error) {
	if c.hybridSearcher == nil {
		return nil, fmt.Errorf("hybrid search not supported by this provider")
	}
	return c.hybridSearcher.HybridQuery(ctx, vector, query, opts...)
}

// QuerySparse queries with sparse vectors
func (c *Client) QuerySparse(ctx context.Context, vector SparseVector, opts ...Option) (*QueryResult, error) {
	if c.sparseSupport == nil {
		return nil, fmt.Errorf("sparse vectors not supported by this provider")
	}
	return c.sparseSupport.QuerySparse(ctx, vector, opts...)
}

// ============================================================================
// Statistics
// ============================================================================

// GetStatistics returns vector store statistics
func (c *Client) GetStatistics(ctx context.Context, opts ...Option) (*Statistics, error) {
	if c.statsProvider == nil {
		return nil, fmt.Errorf("statistics not supported by this provider")
	}
	return c.statsProvider.GetStatistics(ctx, opts...)
}

// ============================================================================
// Capability Checks
// ============================================================================

func (c *Client) SupportsMetadataFiltering() bool { return c.metadataFilterer != nil }
func (c *Client) SupportsBatch() bool             { return c.batchProcessor != nil }
func (c *Client) SupportsNamespaces() bool        { return c.namespaceManager != nil }
func (c *Client) SupportsIndexManagement() bool   { return c.indexManager != nil }
func (c *Client) SupportsHybridSearch() bool      { return c.hybridSearcher != nil }
func (c *Client) SupportsSparseVectors() bool     { return c.sparseSupport != nil }
func (c *Client) SupportsStatistics() bool        { return c.statsProvider != nil }
