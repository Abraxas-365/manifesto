package vstmemory

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/Abraxas-365/manifesto/pkg/ai/vstore"
)

// MemoryVectorStore is an in-memory implementation of vector store
type MemoryVectorStore struct {
	mu         sync.RWMutex
	vectors    map[string]*StoredVector // ID -> Vector
	namespaces map[string][]string      // Namespace -> []IDs
	dimension  int
	metric     vstore.Metric
}

// StoredVector represents a vector with metadata in memory
type StoredVector struct {
	ID        string
	Values    []float32
	Metadata  map[string]any
	Namespace string
}

// NewMemoryVectorStore creates a new in-memory vector store
func NewMemoryVectorStore(dimension int, metric vstore.Metric) *MemoryVectorStore {
	if metric == "" {
		metric = vstore.MetricCosine
	}

	return &MemoryVectorStore{
		vectors:    make(map[string]*StoredVector),
		namespaces: make(map[string][]string),
		dimension:  dimension,
		metric:     metric,
	}
}

// ============================================================================
// VectorStorer Implementation
// ============================================================================

// Upsert inserts or updates vectors
func (m *MemoryVectorStore) Upsert(ctx context.Context, vectors []vstore.Vector, opts ...vstore.Option) error {
	if len(vectors) == 0 {
		return nil
	}

	options := vstore.ApplyOptions(opts...)

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, v := range vectors {
		// Validate dimension
		if len(v.Values) != m.dimension {
			return fmt.Errorf("vector dimension mismatch: expected %d, got %d", m.dimension, len(v.Values))
		}

		namespace := options.Namespace

		// Check if vector already exists and remove from old namespace
		if existing, exists := m.vectors[v.ID]; exists {
			if existing.Namespace != namespace {
				m.removeFromNamespace(existing.Namespace, v.ID)
			}
		}

		// Store vector
		stored := &StoredVector{
			ID:        v.ID,
			Values:    make([]float32, len(v.Values)),
			Metadata:  make(map[string]any),
			Namespace: namespace,
		}

		copy(stored.Values, v.Values)
		for k, val := range v.Metadata {
			stored.Metadata[k] = val
		}

		m.vectors[v.ID] = stored

		// Add to namespace
		m.addToNamespace(namespace, v.ID)
	}

	return nil
}

// Query performs similarity search
func (m *MemoryVectorStore) Query(ctx context.Context, vector []float32, opts ...vstore.Option) (*vstore.QueryResult, error) {
	if len(vector) != m.dimension {
		return nil, fmt.Errorf("query vector dimension mismatch: expected %d, got %d", m.dimension, len(vector))
	}

	options := vstore.ApplyOptions(opts...)

	m.mu.RLock()
	defer m.mu.RUnlock()

	// Get vectors to search (filter by namespace if specified)
	var candidateIDs []string
	if options.Namespace != "" {
		candidateIDs = m.namespaces[options.Namespace]
	} else {
		candidateIDs = make([]string, 0, len(m.vectors))
		for id := range m.vectors {
			candidateIDs = append(candidateIDs, id)
		}
	}

	// Calculate similarities
	type scoredVector struct {
		id    string
		score float32
	}

	scores := make([]scoredVector, 0, len(candidateIDs))

	for _, id := range candidateIDs {
		stored := m.vectors[id]
		if stored == nil {
			continue
		}

		// Apply metadata filter if provided
		if options.Filter != nil && !m.matchesFilter(stored.Metadata, options.Filter) {
			continue
		}

		// Calculate similarity
		score := m.calculateSimilarity(vector, stored.Values)

		// Apply min score filter
		if score >= options.MinScore {
			scores = append(scores, scoredVector{id: id, score: score})
		}
	}

	// Sort by score (descending)
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	// Take top K
	topK := options.TopK
	if topK <= 0 {
		topK = 10
	}
	if topK > len(scores) {
		topK = len(scores)
	}

	// Build result
	matches := make([]vstore.Match, topK)
	for i := 0; i < topK; i++ {
		stored := m.vectors[scores[i].id]
		match := vstore.Match{
			ID:       stored.ID,
			Score:    scores[i].score,
			Metadata: make(map[string]any),
		}

		// Include values if requested
		if options.IncludeValues {
			match.Values = make([]float32, len(stored.Values))
			copy(match.Values, stored.Values)
		}

		// Include metadata if requested
		if options.IncludeMetadata {
			for k, v := range stored.Metadata {
				match.Metadata[k] = v
			}
		}

		matches[i] = match
	}

	return &vstore.QueryResult{
		Matches:   matches,
		Namespace: options.Namespace,
	}, nil
}

// Delete removes vectors by IDs
func (m *MemoryVectorStore) Delete(ctx context.Context, ids []string, opts ...vstore.Option) error {
	if len(ids) == 0 {
		return nil
	}

	options := vstore.ApplyOptions(opts...)

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, id := range ids {
		stored, exists := m.vectors[id]
		if !exists {
			continue
		}

		// Check namespace if specified
		if options.Namespace != "" && stored.Namespace != options.Namespace {
			continue
		}

		// Remove from namespace
		m.removeFromNamespace(stored.Namespace, id)

		// Delete vector
		delete(m.vectors, id)
	}

	return nil
}

// Fetch retrieves vectors by IDs
func (m *MemoryVectorStore) Fetch(ctx context.Context, ids []string, opts ...vstore.Option) ([]vstore.Vector, error) {
	if len(ids) == 0 {
		return []vstore.Vector{}, nil
	}

	options := vstore.ApplyOptions(opts...)

	m.mu.RLock()
	defer m.mu.RUnlock()

	vectors := make([]vstore.Vector, 0, len(ids))

	for _, id := range ids {
		stored, exists := m.vectors[id]
		if !exists {
			continue
		}

		// Check namespace if specified
		if options.Namespace != "" && stored.Namespace != options.Namespace {
			continue
		}

		v := vstore.Vector{
			ID:       stored.ID,
			Values:   make([]float32, len(stored.Values)),
			Metadata: make(map[string]any),
		}

		copy(v.Values, stored.Values)
		for k, val := range stored.Metadata {
			v.Metadata[k] = val
		}

		vectors = append(vectors, v)
	}

	return vectors, nil
}

// ============================================================================
// MetadataFilterer Implementation
// ============================================================================

// QueryWithFilter performs filtered similarity search
func (m *MemoryVectorStore) QueryWithFilter(ctx context.Context, vector []float32, filter vstore.Filter, opts ...vstore.Option) (*vstore.QueryResult, error) {
	opts = append(opts, vstore.WithFilter(&filter))
	return m.Query(ctx, vector, opts...)
}

// ============================================================================
// BatchProcessor Implementation
// ============================================================================

// UpsertBatch upserts vectors in batches
func (m *MemoryVectorStore) UpsertBatch(ctx context.Context, vectors []vstore.Vector, opts ...vstore.Option) (*vstore.BatchResult, error) {
	if err := m.Upsert(ctx, vectors, opts...); err != nil {
		return &vstore.BatchResult{
			FailedCount: len(vectors),
		}, err
	}

	return &vstore.BatchResult{
		SuccessCount: len(vectors),
	}, nil
}

// DeleteBatch deletes multiple vectors
func (m *MemoryVectorStore) DeleteBatch(ctx context.Context, ids []string, opts ...vstore.Option) (*vstore.BatchResult, error) {
	if err := m.Delete(ctx, ids, opts...); err != nil {
		return &vstore.BatchResult{
			FailedCount: len(ids),
		}, err
	}

	return &vstore.BatchResult{
		SuccessCount: len(ids),
	}, nil
}

// ============================================================================
// NamespaceManager Implementation
// ============================================================================

// ListNamespaces returns all namespaces
func (m *MemoryVectorStore) ListNamespaces(ctx context.Context) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	namespaces := make([]string, 0, len(m.namespaces))
	for ns := range m.namespaces {
		if len(m.namespaces[ns]) > 0 {
			namespaces = append(namespaces, ns)
		}
	}

	return namespaces, nil
}

// CreateNamespace creates a namespace (no-op for memory store)
func (m *MemoryVectorStore) CreateNamespace(ctx context.Context, namespace string) error {
	// No-op: namespaces are created implicitly
	return nil
}

// DeleteNamespace deletes a namespace and all its vectors
func (m *MemoryVectorStore) DeleteNamespace(ctx context.Context, namespace string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ids := m.namespaces[namespace]
	for _, id := range ids {
		delete(m.vectors, id)
	}

	delete(m.namespaces, namespace)
	return nil
}

// ============================================================================
// StatisticsProvider Implementation
// ============================================================================

// GetStatistics returns statistics
func (m *MemoryVectorStore) GetStatistics(ctx context.Context, opts ...vstore.Option) (*vstore.Statistics, error) {
	options := vstore.ApplyOptions(opts...)

	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := &vstore.Statistics{
		Dimension: m.dimension,
	}

	if options.Namespace != "" {
		stats.TotalVectorCount = int64(len(m.namespaces[options.Namespace]))
	} else {
		stats.TotalVectorCount = int64(len(m.vectors))
	}

	// Namespace stats
	stats.Namespaces = make([]vstore.NamespaceStats, 0, len(m.namespaces))
	for ns, ids := range m.namespaces {
		if len(ids) > 0 {
			stats.Namespaces = append(stats.Namespaces, vstore.NamespaceStats{
				Name:        ns,
				VectorCount: int64(len(ids)),
			})
		}
	}

	return stats, nil
}

// ============================================================================
// Helper Methods
// ============================================================================

// addToNamespace adds an ID to a namespace
func (m *MemoryVectorStore) addToNamespace(namespace, id string) {
	ids := m.namespaces[namespace]

	// Check if already exists
	for _, existingID := range ids {
		if existingID == id {
			return
		}
	}

	m.namespaces[namespace] = append(ids, id)
}

// removeFromNamespace removes an ID from a namespace
func (m *MemoryVectorStore) removeFromNamespace(namespace, id string) {
	ids := m.namespaces[namespace]
	newIDs := make([]string, 0, len(ids))

	for _, existingID := range ids {
		if existingID != id {
			newIDs = append(newIDs, existingID)
		}
	}

	m.namespaces[namespace] = newIDs
}

// calculateSimilarity calculates similarity between two vectors
func (m *MemoryVectorStore) calculateSimilarity(v1, v2 []float32) float32 {
	switch m.metric {
	case vstore.MetricCosine:
		return cosineSimilarity(v1, v2)
	case vstore.MetricDotProduct:
		return dotProduct(v1, v2)
	case vstore.MetricEuclidean:
		return euclideanSimilarity(v1, v2)
	default:
		return cosineSimilarity(v1, v2)
	}
}

// matchesFilter checks if metadata matches filter
func (m *MemoryVectorStore) matchesFilter(metadata map[string]any, filter *vstore.Filter) bool {
	// Check Must conditions (AND)
	for _, cond := range filter.Must {
		if !m.matchesCondition(metadata, cond) {
			return false
		}
	}

	// Check Should conditions (OR) - at least one must match if any exist
	if len(filter.Should) > 0 {
		matched := false
		for _, cond := range filter.Should {
			if m.matchesCondition(metadata, cond) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check MustNot conditions (NOT)
	for _, cond := range filter.MustNot {
		if m.matchesCondition(metadata, cond) {
			return false
		}
	}

	return true
}

// matchesCondition checks if metadata matches a single condition
func (m *MemoryVectorStore) matchesCondition(metadata map[string]any, cond vstore.Condition) bool {
	value, exists := metadata[cond.Field]

	switch cond.Operator {
	case vstore.OpExists:
		return exists

	case vstore.OpEqual:
		if !exists {
			return false
		}
		return fmt.Sprintf("%v", value) == fmt.Sprintf("%v", cond.Value)

	case vstore.OpNotEqual:
		if !exists {
			return true
		}
		return fmt.Sprintf("%v", value) != fmt.Sprintf("%v", cond.Value)

	case vstore.OpGreaterThan:
		if !exists {
			return false
		}
		return compareValues(value, cond.Value) > 0

	case vstore.OpLessThan:
		if !exists {
			return false
		}
		return compareValues(value, cond.Value) < 0

	case vstore.OpGreaterThanOrEqual:
		if !exists {
			return false
		}
		return compareValues(value, cond.Value) >= 0

	case vstore.OpLessThanOrEqual:
		if !exists {
			return false
		}
		return compareValues(value, cond.Value) <= 0

	case vstore.OpContains:
		if !exists {
			return false
		}
		str := fmt.Sprintf("%v", value)
		substr := fmt.Sprintf("%v", cond.Value)
		return strings.Contains(strings.ToLower(str), strings.ToLower(substr))

	default:
		return false
	}
}

// compareValues compares two values
func compareValues(a, b any) int {
	// Try to convert to float64 for numeric comparison
	aFloat, aOk := toFloat64(a)
	bFloat, bOk := toFloat64(b)

	if aOk && bOk {
		if aFloat < bFloat {
			return -1
		} else if aFloat > bFloat {
			return 1
		}
		return 0
	}

	// Fallback to string comparison
	aStr := fmt.Sprintf("%v", a)
	bStr := fmt.Sprintf("%v", b)
	return strings.Compare(aStr, bStr)
}

func toFloat64(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case int32:
		return float64(val), true
	default:
		return 0, false
	}
}

// ============================================================================
// Similarity Functions
// ============================================================================

// cosineSimilarity calculates cosine similarity
func cosineSimilarity(v1, v2 []float32) float32 {
	if len(v1) != len(v2) {
		return 0
	}

	var dotProduct, normA, normB float32
	for i := range v1 {
		dotProduct += v1[i] * v2[i]
		normA += v1[i] * v1[i]
		normB += v2[i] * v2[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}

// dotProduct calculates dot product
func dotProduct(v1, v2 []float32) float32 {
	if len(v1) != len(v2) {
		return 0
	}

	var sum float32
	for i := range v1 {
		sum += v1[i] * v2[i]
	}
	return sum
}

// euclideanSimilarity converts euclidean distance to similarity (0-1 range)
func euclideanSimilarity(v1, v2 []float32) float32 {
	if len(v1) != len(v2) {
		return 0
	}

	var sum float32
	for i := range v1 {
		diff := v1[i] - v2[i]
		sum += diff * diff
	}

	distance := float32(math.Sqrt(float64(sum)))
	return 1.0 / (1.0 + distance)
}

// ============================================================================
// Utility Methods
// ============================================================================

// Clear removes all vectors
func (m *MemoryVectorStore) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.vectors = make(map[string]*StoredVector)
	m.namespaces = make(map[string][]string)
}

// Count returns the total number of vectors
func (m *MemoryVectorStore) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.vectors)
}
