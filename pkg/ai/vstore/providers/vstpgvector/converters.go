package vstpgvector

import (
	"database/sql"
	"fmt"

	"github.com/Abraxas-365/manifesto/pkg/ai/vstore"
	"github.com/Abraxas-365/manifesto/pkg/errx"
)

// ============================================================================
// Vector Conversions
// ============================================================================

// ToVstoreVector converts a database record to a vstore.Vector
func ToVstoreVector(id string, pgVector Vector, metadata Metadata) vstore.Vector {
	return vstore.Vector{
		ID:       id,
		Values:   []float32(pgVector),
		Metadata: map[string]any(metadata),
	}
}

// ToVstoreVectors converts multiple records to vstore.Vector slice
func ToVstoreVectors(records []VectorRecord) []vstore.Vector {
	vectors := make([]vstore.Vector, len(records))
	for i, record := range records {
		vectors[i] = ToVstoreVector(record.ID, record.Vector, record.Metadata)
	}
	return vectors
}

// ToPgVector converts a vstore.Vector to database types
func ToPgVector(v vstore.Vector) (id string, vector Vector, metadata Metadata) {
	return v.ID, Vector(v.Values), Metadata(v.Metadata)
}

// ============================================================================
// Query Result Conversions
// ============================================================================

// QueryResultBuilder helps build query results from SQL rows
type QueryResultBuilder struct {
	matches         []vstore.Match
	namespace       string
	includeValues   bool
	includeMetadata bool
	metric          DistanceMetric
}

// NewQueryResultBuilder creates a new query result builder
func NewQueryResultBuilder(namespace string, includeValues, includeMetadata bool, metric DistanceMetric) *QueryResultBuilder {
	return &QueryResultBuilder{
		matches:         make([]vstore.Match, 0),
		namespace:       namespace,
		includeValues:   includeValues,
		includeMetadata: includeMetadata,
		metric:          metric,
	}
}

// ScanRow scans a SQL row into a match
func (b *QueryResultBuilder) ScanRow(rows *sql.Rows) error {
	match := vstore.Match{}

	scanArgs := []any{&match.ID}

	var pgVector Vector
	if b.includeValues {
		scanArgs = append(scanArgs, &pgVector)
	}

	var metadata Metadata
	if b.includeMetadata {
		scanArgs = append(scanArgs, &metadata)
	}

	var distance float32
	scanArgs = append(scanArgs, &distance)

	if err := rows.Scan(scanArgs...); err != nil {
		return err
	}

	if b.includeValues {
		match.Values = []float32(pgVector)
	}
	if b.includeMetadata {
		match.Metadata = map[string]any(metadata)
	}

	// Convert distance to similarity score
	match.Score = ConvertDistanceToScore(distance, b.metric)

	b.matches = append(b.matches, match)
	return nil
}

// Build creates the final QueryResult
func (b *QueryResultBuilder) Build() *vstore.QueryResult {
	return &vstore.QueryResult{
		Matches:   b.matches,
		Namespace: b.namespace,
	}
}

// FilterByMinScore filters matches by minimum score
func (b *QueryResultBuilder) FilterByMinScore(minScore float32) {
	if minScore <= 0 {
		return
	}

	filtered := make([]vstore.Match, 0, len(b.matches))
	for _, match := range b.matches {
		if match.Score >= minScore {
			filtered = append(filtered, match)
		}
	}
	b.matches = filtered
}

// ============================================================================
// Distance/Score Conversions
// ============================================================================

// ConvertDistanceToScore converts distance to similarity score based on metric
func ConvertDistanceToScore(distance float32, metric DistanceMetric) float32 {
	switch metric {
	case DistanceCosine:
		// Cosine distance is in [0, 2], where 0 is identical
		// Convert to similarity score in [0, 1]
		return 1.0 - (distance / 2.0)
	case DistanceInnerProduct:
		// Inner product: higher (less negative) is better
		// Since pgvector returns negative inner product as distance
		return -distance
	case DistanceL2:
		// L2 distance: 0 is identical, larger is more different
		// Convert to similarity score in [0, 1]
		return 1.0 / (1.0 + distance)
	default:
		return 1.0 / (1.0 + distance)
	}
}

// ConvertScoreToDistance converts similarity score to distance
func ConvertScoreToDistance(score float32, metric DistanceMetric) float32 {
	switch metric {
	case DistanceCosine:
		return (1.0 - score) * 2.0
	case DistanceInnerProduct:
		return -score
	case DistanceL2:
		if score >= 1.0 {
			return 0
		}
		return (1.0 / score) - 1.0
	default:
		if score >= 1.0 {
			return 0
		}
		return (1.0 / score) - 1.0
	}
}

// ============================================================================
// Metric Conversions
// ============================================================================

// VstoreMetricToPg converts vstore.Metric to pgvector DistanceMetric
func VstoreMetricToPg(metric vstore.Metric) DistanceMetric {
	switch metric {
	case vstore.MetricCosine:
		return DistanceCosine
	case vstore.MetricDotProduct:
		return DistanceInnerProduct
	case vstore.MetricEuclidean:
		return DistanceL2
	default:
		return DistanceCosine // Default
	}
}

// PgMetricToVstore converts pgvector DistanceMetric to vstore.Metric
func PgMetricToVstore(metric DistanceMetric) vstore.Metric {
	switch metric {
	case DistanceCosine:
		return vstore.MetricCosine
	case DistanceInnerProduct:
		return vstore.MetricDotProduct
	case DistanceL2:
		return vstore.MetricEuclidean
	default:
		return vstore.MetricCosine
	}
}

// GetDistanceOperator returns the pgvector distance operator for a metric
func GetDistanceOperator(metric DistanceMetric) string {
	switch metric {
	case DistanceCosine:
		return "<=>" // Cosine distance
	case DistanceInnerProduct:
		return "<#>" // Negative inner product
	case DistanceL2:
		return "<->" // L2 distance
	default:
		return "<->" // Default to L2
	}
}

// ============================================================================
// Index Conversions
// ============================================================================

// ToVstoreIndexInfo converts TableInfo to vstore.IndexInfo slice
func ToVstoreIndexInfo(tableInfo *TableInfo) []vstore.IndexInfo {
	indexes := make([]vstore.IndexInfo, 0, len(tableInfo.Indexes))

	for _, idx := range tableInfo.Indexes {
		info := vstore.IndexInfo{
			Name:             idx.IndexName,
			Dimension:        tableInfo.Dimension,
			TotalVectorCount: tableInfo.VectorCount,
			Status:           "ready",
			Metadata: map[string]any{
				"table_name": tableInfo.TableName,
				"schema":     tableInfo.Schema,
				"index_type": idx.IndexType,
				"is_valid":   idx.IsValid,
			},
		}

		if idx.Size != "" {
			info.Metadata["size"] = idx.Size
		}

		indexes = append(indexes, info)
	}

	return indexes
}

// ToVstoreIndexConfig converts vstore.IndexConfig to pgvector IndexConfig
func ToVstoreIndexConfig(config vstore.IndexConfig, tableName string, defaultType IndexType) IndexConfig {
	pgConfig := IndexConfig{
		IndexName:      config.Name,
		TableName:      tableName,
		IndexType:      defaultType,
		DistanceMetric: VstoreMetricToPg(config.Metric),
	}

	if pgConfig.IndexName == "" {
		pgConfig.IndexName = fmt.Sprintf("idx_%s_vector", tableName)
	}

	// Set provider-specific options if available
	if config.PodType != "" {
		// HNSW or IVFFlat based on pod type hint
		if config.PodType == "hnsw" {
			pgConfig.IndexType = IndexTypeHNSW
		} else if config.PodType == "ivfflat" {
			pgConfig.IndexType = IndexTypeIVFFlat
		}
	}

	return pgConfig
}

// ============================================================================
// Statistics Conversions
// ============================================================================

// ToVstoreStatistics converts database statistics to vstore.Statistics
func ToVstoreStatistics(totalCount int64, dimension int, namespaces []NamespaceStats) *vstore.Statistics {
	stats := &vstore.Statistics{
		TotalVectorCount: totalCount,
		Dimension:        dimension,
		IndexFullness:    0, // Not applicable for pgvector
	}

	if len(namespaces) > 0 {
		stats.Namespaces = make([]vstore.NamespaceStats, len(namespaces))
		for i, ns := range namespaces {
			stats.Namespaces[i] = vstore.NamespaceStats{
				Name:        ns.Name,
				VectorCount: ns.VectorCount,
			}
		}
	}

	return stats
}

// NamespaceStats represents namespace-level statistics
type NamespaceStats struct {
	Name        string
	VectorCount int64
}

// ============================================================================
// Batch Conversions
// ============================================================================

// SplitIntoBatches splits a slice of vectors into batches
func SplitIntoBatches(vectors []vstore.Vector, batchSize int) [][]vstore.Vector {
	if batchSize <= 0 {
		batchSize = 100
	}

	batches := make([][]vstore.Vector, 0)
	for i := 0; i < len(vectors); i += batchSize {
		end := i + batchSize
		if end > len(vectors) {
			end = len(vectors)
		}
		batches = append(batches, vectors[i:end])
	}

	return batches
}

// SplitIDsIntoBatches splits a slice of IDs into batches
func SplitIDsIntoBatches(ids []string, batchSize int) [][]string {
	if batchSize <= 0 {
		batchSize = 100
	}

	batches := make([][]string, 0)
	for i := 0; i < len(ids); i += batchSize {
		end := i + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		batches = append(batches, ids[i:end])
	}

	return batches
}

// ============================================================================
// Error Conversions
// ============================================================================

// ConvertBatchErrors converts multiple errors into a BatchResult
func ConvertBatchErrors(errors map[string]error) *vstore.BatchResult {
	result := &vstore.BatchResult{
		FailedCount: len(errors),
		Errors:      make([]vstore.BatchError, 0, len(errors)),
	}

	for id, err := range errors {
		result.Errors = append(result.Errors, vstore.BatchError{
			ID:    id,
			Error: err.Error(),
		})
	}

	return result
}

// MergeBatchResults merges multiple batch results
func MergeBatchResults(results ...*vstore.BatchResult) *vstore.BatchResult {
	merged := &vstore.BatchResult{
		Errors: make([]vstore.BatchError, 0),
	}

	for _, result := range results {
		merged.SuccessCount += result.SuccessCount
		merged.FailedCount += result.FailedCount
		merged.Errors = append(merged.Errors, result.Errors...)
	}

	return merged
}

// ============================================================================
// Validation Helpers
// ============================================================================

// ValidateVector validates a vector before database operations
func ValidateVector(v vstore.Vector, expectedDimension int) *errx.Error {
	if v.ID == "" {
		return errorRegistry.New(ErrEmptyVectorID)
	}

	if len(v.Values) != expectedDimension {
		return errorRegistry.New(ErrInvalidVectorDimension).
			WithDetail("expected", expectedDimension).
			WithDetail("got", len(v.Values)).
			WithDetail("vector_id", v.ID)
	}

	return nil
}

// ValidateVectors validates multiple vectors
func ValidateVectors(vectors []vstore.Vector, expectedDimension int) *errx.Error {
	for i, v := range vectors {
		if err := ValidateVector(v, expectedDimension); err != nil {
			return err.WithDetail("index", i)
		}
	}
	return nil
}

// ============================================================================
// Namespace Helpers
// ============================================================================

// NormalizeNamespace normalizes a namespace string
func NormalizeNamespace(namespace string) string {
	if namespace == "" {
		return "" // Empty namespace is valid
	}
	// Could add more normalization logic here
	return namespace
}

// ============================================================================
// SQL Parameter Helpers
// ============================================================================

// BuildPlaceholders builds SQL placeholders ($1, $2, ..., $n)
func BuildPlaceholders(start, count int) string {
	if count <= 0 {
		return ""
	}

	placeholders := make([]string, count)
	for i := 0; i < count; i++ {
		placeholders[i] = fmt.Sprintf("$%d", start+i)
	}
	return fmt.Sprintf("(%s)", joinStrings(placeholders, ","))
}

// BuildBatchPlaceholders builds multiple value sets for batch inserts
// e.g., ($1,$2,$3),($4,$5,$6),($7,$8,$9)
func BuildBatchPlaceholders(batchSize, fieldsPerRow, startArg int) string {
	if batchSize <= 0 || fieldsPerRow <= 0 {
		return ""
	}

	sets := make([]string, batchSize)
	argNum := startArg

	for i := 0; i < batchSize; i++ {
		placeholders := make([]string, fieldsPerRow)
		for j := 0; j < fieldsPerRow; j++ {
			placeholders[j] = fmt.Sprintf("$%d", argNum)
			argNum++
		}
		sets[i] = "(" + joinStrings(placeholders, ",") + ")"
	}

	return joinStrings(sets, ",")
}

// joinStrings is a helper to join strings with a separator
func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}

// ============================================================================
// Metadata Helpers
// ============================================================================

// MergeMetadata merges multiple metadata maps
func MergeMetadata(base, override map[string]any) map[string]any {
	result := make(map[string]any, len(base)+len(override))

	for k, v := range base {
		result[k] = v
	}

	for k, v := range override {
		result[k] = v
	}

	return result
}

// ExtractMetadataField safely extracts a field from metadata
func ExtractMetadataField(metadata map[string]any, field string) (any, bool) {
	if metadata == nil {
		return nil, false
	}
	val, ok := metadata[field]
	return val, ok
}

// ============================================================================
// Index Type Helpers
// ============================================================================

// DetermineIndexTypeFromDef parses index definition to determine type
func DetermineIndexTypeFromDef(indexDef string) string {
	if contains(indexDef, "ivfflat") {
		return string(IndexTypeIVFFlat)
	} else if contains(indexDef, "hnsw") {
		return string(IndexTypeHNSW)
	}
	return "unknown"
}

// GetDefaultIndexParams returns default parameters for index type
func GetDefaultIndexParams(indexType IndexType) map[string]int {
	switch indexType {
	case IndexTypeIVFFlat:
		return map[string]int{
			"lists": 100,
		}
	case IndexTypeHNSW:
		return map[string]int{
			"m":               16,
			"ef_construction": 64,
		}
	default:
		return map[string]int{}
	}
}
