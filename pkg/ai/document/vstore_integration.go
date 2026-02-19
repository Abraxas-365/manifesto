package document

import (
	"context"
	"fmt"
	"io"

	"github.com/Abraxas-365/manifesto/pkg/ai/embedding"
	"github.com/Abraxas-365/manifesto/pkg/ai/vstore"
)

// ============================================================================
// Embedder Interface - wraps embedding.Embedder with dimension info
// ============================================================================

// Embedder generates embeddings for documents
// This is a simple wrapper around embedding.Embedder that adds dimension info
type Embedder interface {
	embedding.Embedder // Embed the existing interface
	Dimensions() int   // Add dimension info
}

// ============================================================================
// Default Embedder Implementation
// ============================================================================

// DefaultEmbedder wraps an embedding.Embedder with dimension info and default options
type DefaultEmbedder struct {
	embedder   embedding.Embedder
	dimensions int
	options    []embedding.Option
}

// NewEmbedder creates a document embedder from an embedding.Embedder
func NewEmbedder(embedder embedding.Embedder, dimensions int, opts ...embedding.Option) Embedder {
	return &DefaultEmbedder{
		embedder:   embedder,
		dimensions: dimensions,
		options:    opts,
	}
}

// EmbedDocuments implements embedding.Embedder
func (e *DefaultEmbedder) EmbedDocuments(ctx context.Context, texts []string, opts ...embedding.Option) ([]embedding.Embedding, error) {
	// Merge default options with provided options
	allOpts := append(e.options, opts...)
	return e.embedder.EmbedDocuments(ctx, texts, allOpts...)
}

// EmbedQuery implements embedding.Embedder
func (e *DefaultEmbedder) EmbedQuery(ctx context.Context, text string, opts ...embedding.Option) (embedding.Embedding, error) {
	// Merge default options with provided options
	allOpts := append(e.options, opts...)
	return e.embedder.EmbedQuery(ctx, text, allOpts...)
}

// Dimensions returns the embedding dimension
func (e *DefaultEmbedder) Dimensions() int {
	return e.dimensions
}

// ============================================================================
// Helper Functions for Vector Extraction
// ============================================================================

// ExtractVectors extracts float32 vectors from embeddings
func ExtractVectors(embeddings []embedding.Embedding) [][]float32 {
	vectors := make([][]float32, len(embeddings))
	for i, emb := range embeddings {
		vectors[i] = emb.Vector
	}
	return vectors
}

// ExtractVector extracts a single vector from an embedding
func ExtractVector(emb embedding.Embedding) []float32 {
	return emb.Vector
}

// ============================================================================
// Document Store - combines document processing with vector storage
// ============================================================================

// DocumentStore manages documents in a vector store
type DocumentStore struct {
	vectorStore *vstore.Client
	embedder    Embedder
	namespace   string
	batchSize   int
}

// NewDocumentStore creates a new document store
func NewDocumentStore(vectorStore *vstore.Client, embedder Embedder) *DocumentStore {
	return &DocumentStore{
		vectorStore: vectorStore,
		embedder:    embedder,
		batchSize:   100,
	}
}

// WithNamespace sets the namespace for documents
func (ds *DocumentStore) WithNamespace(namespace string) *DocumentStore {
	ds.namespace = namespace
	return ds
}

// WithBatchSize sets the batch size for ingestion
func (ds *DocumentStore) WithBatchSize(size int) *DocumentStore {
	ds.batchSize = size
	return ds
}

// ============================================================================
// Add Documents
// ============================================================================

// AddDocuments adds documents to the vector store
func (ds *DocumentStore) AddDocuments(ctx context.Context, docs []*Document) error {
	if len(docs) == 0 {
		return nil
	}

	// Generate embeddings for documents that don't have them
	docsToEmbed := make([]string, 0)
	embedIndices := make([]int, 0)

	for i, doc := range docs {
		if doc.Embedding == nil {
			docsToEmbed = append(docsToEmbed, doc.Content)
			embedIndices = append(embedIndices, i)
		}
	}

	// Generate embeddings if needed
	if len(docsToEmbed) > 0 {
		// Use the embedding.Embedder interface
		embeddings, err := ds.embedder.EmbedDocuments(ctx, docsToEmbed)
		if err != nil {
			return fmt.Errorf("failed to generate embeddings: %w", err)
		}

		// Extract vectors and assign to documents
		for i, idx := range embedIndices {
			docs[idx].Embedding = embeddings[i].Vector
		}
	}

	// Convert documents to vectors
	vectors := make([]vstore.Vector, len(docs))
	for i, doc := range docs {
		vectors[i] = ds.documentToVector(doc)
	}

	// Upsert in batches
	for i := 0; i < len(vectors); i += ds.batchSize {
		end := i + ds.batchSize
		if end > len(vectors) {
			end = len(vectors)
		}

		batch := vectors[i:end]
		opts := []vstore.Option{}
		if ds.namespace != "" {
			opts = append(opts, vstore.WithNamespace(ds.namespace))
		}

		if err := ds.vectorStore.Upsert(ctx, batch, opts...); err != nil {
			return fmt.Errorf("failed to upsert batch: %w", err)
		}
	}

	return nil
}

// AddDocumentsStream adds documents from a stream (memory efficient)
func (ds *DocumentStore) AddDocumentsStream(ctx context.Context, stream DocumentStream) error {
	batch := make([]*Document, 0, ds.batchSize)

	for {
		doc, err := stream.Next()
		if err == io.EOF {
			// Process final batch
			if len(batch) > 0 {
				if err := ds.AddDocuments(ctx, batch); err != nil {
					return err
				}
			}
			break
		}
		if err != nil {
			return err
		}

		batch = append(batch, doc)

		// Process batch when full
		if len(batch) >= ds.batchSize {
			if err := ds.AddDocuments(ctx, batch); err != nil {
				return err
			}
			batch = batch[:0] // Reset batch
		}
	}

	return nil
}

// ============================================================================
// Search Documents
// ============================================================================

// SearchRequest contains search parameters
type SearchRequest struct {
	Query     string
	TopK      int
	Filter    *vstore.Filter
	MinScore  float32
	Namespace string
}

// SearchResult contains search results
type SearchResult struct {
	Documents []*Document
	Scores    []float32
}

// Search searches for similar documents
func (ds *DocumentStore) Search(ctx context.Context, req SearchRequest) (*SearchResult, error) {
	// Generate query embedding using embedding.Embedder
	queryEmb, err := ds.embedder.EmbedQuery(ctx, req.Query)
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}

	// Extract the vector
	queryEmbedding := queryEmb.Vector

	// Set defaults
	if req.TopK == 0 {
		req.TopK = 10
	}

	namespace := req.Namespace
	if namespace == "" {
		namespace = ds.namespace
	}

	// Build query options
	opts := []vstore.Option{
		vstore.WithTopK(req.TopK),
		vstore.WithIncludeMetadata(true),
		vstore.WithIncludeValues(false),
	}

	if namespace != "" {
		opts = append(opts, vstore.WithNamespace(namespace))
	}

	if req.Filter != nil {
		opts = append(opts, vstore.WithFilter(req.Filter))
	}

	if req.MinScore > 0 {
		opts = append(opts, vstore.WithMinScore(req.MinScore))
	}

	// Execute search
	results, err := ds.vectorStore.Query(ctx, queryEmbedding, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to query vector store: %w", err)
	}

	// Convert results to documents
	docs := make([]*Document, len(results.Matches))
	scores := make([]float32, len(results.Matches))

	for i, match := range results.Matches {
		docs[i] = ds.vectorToDocument(match)
		scores[i] = match.Score
	}

	return &SearchResult{
		Documents: docs,
		Scores:    scores,
	}, nil
}

// SearchStream searches and returns results as a stream
func (ds *DocumentStore) SearchStream(ctx context.Context, req SearchRequest) (DocumentStream, error) {
	result, err := ds.Search(ctx, req)
	if err != nil {
		return nil, err
	}

	// Create stream from results
	index := 0
	return DocumentStreamFunc(func() (*Document, error) {
		if index >= len(result.Documents) {
			return nil, io.EOF
		}
		doc := result.Documents[index]
		index++
		return doc, nil
	}), nil
}

// ============================================================================
// Delete Documents
// ============================================================================

// DeleteDocuments deletes documents by ID
func (ds *DocumentStore) DeleteDocuments(ctx context.Context, ids []string) error {
	opts := []vstore.Option{}
	if ds.namespace != "" {
		opts = append(opts, vstore.WithNamespace(ds.namespace))
	}

	return ds.vectorStore.Delete(ctx, ids, opts...)
}

// DeleteByFilter deletes documents matching a filter
func (ds *DocumentStore) DeleteByFilter(ctx context.Context, filter *vstore.Filter) error {
	// First, search for matching documents
	// Note: This is a simplified implementation
	// In production, you might want a direct delete-by-filter API

	result, err := ds.Search(ctx, SearchRequest{
		Query:  "", // Empty query to match filter only
		TopK:   10000,
		Filter: filter,
	})
	if err != nil {
		return err
	}

	// Extract IDs
	ids := make([]string, len(result.Documents))
	for i, doc := range result.Documents {
		ids[i] = doc.ID
	}

	// Delete by IDs
	return ds.DeleteDocuments(ctx, ids)
}

// ============================================================================
// Update Documents
// ============================================================================

// UpdateDocument updates a document's metadata and/or content
func (ds *DocumentStore) UpdateDocument(ctx context.Context, doc *Document) error {
	// If content changed, regenerate embedding
	if doc.Embedding == nil {
		emb, err := ds.embedder.EmbedQuery(ctx, doc.Content)
		if err != nil {
			return fmt.Errorf("failed to generate embedding: %w", err)
		}
		doc.Embedding = emb.Vector
	}

	// Upsert the document
	vector := ds.documentToVector(doc)

	opts := []vstore.Option{}
	if ds.namespace != "" {
		opts = append(opts, vstore.WithNamespace(ds.namespace))
	}

	return ds.vectorStore.Upsert(ctx, []vstore.Vector{vector}, opts...)
}

// ============================================================================
// Get Documents
// ============================================================================

// GetDocuments retrieves documents by ID
func (ds *DocumentStore) GetDocuments(ctx context.Context, ids []string) ([]*Document, error) {
	opts := []vstore.Option{}
	if ds.namespace != "" {
		opts = append(opts, vstore.WithNamespace(ds.namespace))
	}

	vectors, err := ds.vectorStore.Fetch(ctx, ids, opts...)
	if err != nil {
		return nil, err
	}

	docs := make([]*Document, len(vectors))
	for i, v := range vectors {
		docs[i] = &Document{
			ID:        v.ID,
			Content:   getContentFromMetadata(v.Metadata),
			Metadata:  v.Metadata,
			Embedding: v.Values,
		}
	}

	return docs, nil
}

// ============================================================================
// Statistics
// ============================================================================

// GetStats returns statistics about the document store
func (ds *DocumentStore) GetStats(ctx context.Context) (*vstore.Statistics, error) {
	opts := []vstore.Option{}
	if ds.namespace != "" {
		opts = append(opts, vstore.WithNamespace(ds.namespace))
	}

	return ds.vectorStore.GetStatistics(ctx, opts...)
}

// ============================================================================
// Helper Methods
// ============================================================================

// documentToVector converts a document to a vector
func (ds *DocumentStore) documentToVector(doc *Document) vstore.Vector {
	// Ensure content is in metadata
	metadata := make(map[string]any, len(doc.Metadata)+1)
	for k, v := range doc.Metadata {
		metadata[k] = v
	}
	metadata["content"] = doc.Content

	return vstore.Vector{
		ID:       doc.ID,
		Values:   doc.Embedding,
		Metadata: metadata,
	}
}

// vectorToDocument converts a vector to a document
func (ds *DocumentStore) vectorToDocument(match vstore.Match) *Document {
	content := getContentFromMetadata(match.Metadata)

	return &Document{
		ID:        match.ID,
		Content:   content,
		Metadata:  match.Metadata,
		Embedding: match.Values,
	}
}

func getContentFromMetadata(metadata map[string]any) string {
	if content, ok := metadata["content"].(string); ok {
		return content
	}
	if text, ok := metadata["text"].(string); ok {
		return text
	}
	return ""
}
