package document

import (
	"context"
	"fmt"
	"strings"
)

// ============================================================================
// Retriever - retrieves relevant documents for RAG
// ============================================================================

// Retriever retrieves relevant documents for a query
type Retriever struct {
	store           *DocumentStore
	searchType      SearchType
	topK            int
	minScore        float32
	mmrLambda       float32 // For MMR (Maximal Marginal Relevance)
	reranker        Reranker
	compressionFunc CompressionFunc
}

// SearchType defines the retrieval strategy
type SearchType string

const (
	SearchTypeSimilarity SearchType = "similarity" // Basic similarity search
	SearchTypeMMR        SearchType = "mmr"        // Maximal Marginal Relevance
	SearchTypeThreshold  SearchType = "threshold"  // Score threshold
	SearchTypeRerank     SearchType = "rerank"     // With reranking
)

// Reranker reranks search results
type Reranker interface {
	Rerank(ctx context.Context, query string, docs []*Document) ([]*Document, error)
}

// CompressionFunc compresses document content
type CompressionFunc func(ctx context.Context, query string, doc *Document) string

// NewRetriever creates a new retriever
func NewRetriever(store *DocumentStore) *Retriever {
	return &Retriever{
		store:      store,
		searchType: SearchTypeSimilarity,
		topK:       4,
		minScore:   0.0,
		mmrLambda:  0.5,
	}
}

// WithSearchType sets the search type
func (r *Retriever) WithSearchType(searchType SearchType) *Retriever {
	r.searchType = searchType
	return r
}

// WithTopK sets the number of documents to retrieve
func (r *Retriever) WithTopK(k int) *Retriever {
	r.topK = k
	return r
}

// WithMinScore sets the minimum similarity score
func (r *Retriever) WithMinScore(score float32) *Retriever {
	r.minScore = score
	return r
}

// WithReranker sets a reranker
func (r *Retriever) WithReranker(reranker Reranker) *Retriever {
	r.reranker = reranker
	r.searchType = SearchTypeRerank
	return r
}

// WithCompression sets a compression function
func (r *Retriever) WithCompression(fn CompressionFunc) *Retriever {
	r.compressionFunc = fn
	return r
}

// Retrieve retrieves relevant documents
func (r *Retriever) Retrieve(ctx context.Context, query string) ([]*Document, error) {
	// Initial search
	searchReq := SearchRequest{
		Query:    query,
		TopK:     r.topK * 2, // Fetch more for reranking/MMR
		MinScore: r.minScore,
	}

	result, err := r.store.Search(ctx, searchReq)
	if err != nil {
		return nil, err
	}

	docs := result.Documents

	// Apply search strategy
	switch r.searchType {
	case SearchTypeMMR:
		docs = r.applyMMR(docs, result.Scores, r.topK)

	case SearchTypeRerank:
		if r.reranker != nil {
			docs, err = r.reranker.Rerank(ctx, query, docs)
			if err != nil {
				return nil, err
			}
		}
		if len(docs) > r.topK {
			docs = docs[:r.topK]
		}

	default:
		// Simple similarity - just take topK
		if len(docs) > r.topK {
			docs = docs[:r.topK]
		}
	}

	// Apply compression if set
	if r.compressionFunc != nil {
		for i, doc := range docs {
			compressed := r.compressionFunc(ctx, query, doc)
			docs[i] = doc.Clone()
			docs[i].Content = compressed
		}
	}

	return docs, nil
}

// applyMMR applies Maximal Marginal Relevance
func (r *Retriever) applyMMR(docs []*Document, scores []float32, k int) []*Document {
	if len(docs) <= k {
		return docs
	}

	selected := make([]*Document, 0, k)
	selectedIndices := make(map[int]bool)

	// Select first document (highest similarity)
	selected = append(selected, docs[0])
	selectedIndices[0] = true

	// Select remaining documents using MMR
	for len(selected) < k {
		maxMMR := float32(-1)
		maxIdx := -1

		for i, doc := range docs {
			if selectedIndices[i] {
				continue
			}

			// Calculate MMR score
			querySimScore := scores[i]

			// Calculate max similarity to selected docs
			maxDocSim := float32(0)
			for j := range selected {
				if selectedIndices[j] {
					sim := r.cosineSimilarity(doc.Embedding, docs[j].Embedding)
					if sim > maxDocSim {
						maxDocSim = sim
					}
				}
			}

			// MMR = lambda * query_sim - (1-lambda) * doc_sim
			mmr := r.mmrLambda*querySimScore - (1-r.mmrLambda)*maxDocSim

			if mmr > maxMMR {
				maxMMR = mmr
				maxIdx = i
			}
		}

		if maxIdx >= 0 {
			selected = append(selected, docs[maxIdx])
			selectedIndices[maxIdx] = true
		} else {
			break
		}
	}

	return selected
}

// cosineSimilarity calculates cosine similarity between two vectors
func (r *Retriever) cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}

	var dotProduct, normA, normB float32
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (sqrt(normA) * sqrt(normB))
}

func sqrt(x float32) float32 {
	// Simple sqrt implementation
	if x == 0 {
		return 0
	}
	z := x
	for i := 0; i < 10; i++ {
		z = (z + x/z) / 2
	}
	return z
}

// ============================================================================
// Context Builder - builds context for RAG
// ============================================================================

// ContextBuilder builds context from retrieved documents
type ContextBuilder struct {
	separator     string
	includeSource bool
	maxLength     int
	template      string
}

// NewContextBuilder creates a new context builder
func NewContextBuilder() *ContextBuilder {
	return &ContextBuilder{
		separator:     "\n\n",
		includeSource: true,
		maxLength:     0, // No limit
		template:      "Content: {{.Content}}\nSource: {{.Source}}",
	}
}

// WithSeparator sets the separator between documents
func (cb *ContextBuilder) WithSeparator(sep string) *ContextBuilder {
	cb.separator = sep
	return cb
}

// WithMaxLength sets the maximum context length
func (cb *ContextBuilder) WithMaxLength(length int) *ContextBuilder {
	cb.maxLength = length
	return cb
}

// Build builds context from documents
func (cb *ContextBuilder) Build(docs []*Document) string {
	var builder strings.Builder
	currentLength := 0

	for i, doc := range docs {
		var docText string

		if cb.includeSource {
			if source, ok := doc.GetMetadataString(MetadataSource); ok {
				docText = fmt.Sprintf("Source: %s\n%s", source, doc.Content)
			} else {
				docText = doc.Content
			}
		} else {
			docText = doc.Content
		}

		// Check length limit
		if cb.maxLength > 0 && currentLength+len(docText) > cb.maxLength {
			// Truncate if needed
			remaining := cb.maxLength - currentLength
			if remaining > 0 {
				docText = docText[:remaining] + "..."
				builder.WriteString(docText)
			}
			break
		}

		if i > 0 {
			builder.WriteString(cb.separator)
		}
		builder.WriteString(docText)
		currentLength += len(docText) + len(cb.separator)
	}

	return builder.String()
}
