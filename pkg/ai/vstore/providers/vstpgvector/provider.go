package vstpgvector

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Abraxas-365/manifesto/pkg/ai/vstore"
	"github.com/Abraxas-365/manifesto/pkg/errx"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq" // PostgreSQL driver
)

const (
	DefaultSchema            = "public"
	DefaultTableName         = "vectors"
	DefaultMaxConnections    = 25
	DefaultConnectionTimeout = 10 * time.Second
	DefaultBatchSize         = 100
)

// PgVectorProvider implements vector store for PostgreSQL with pgvector
type PgVectorProvider struct {
	db     *sqlx.DB
	client *DBClient

	// Configuration
	schema             string
	tableName          string
	dimension          int
	maxConnections     int
	connectionTimeout  time.Duration
	defaultIndexType   IndexType
	defaultMetric      DistanceMetric
	autoCreateTable    bool
	useNamespaceColumn bool
	batchSize          int

	// Track if we own the connection (should close it)
	ownsConnection bool
}

// NewPgVectorProvider creates a new pgvector provider from a connection string
func NewPgVectorProvider(connStr string, dimension int, opts ...ProviderOption) (*PgVectorProvider, *errx.Error) {
	if connStr == "" {
		return nil, errorRegistry.New(ErrMissingConfig).
			WithDetail("error", "connection string is required")
	}

	if dimension <= 0 {
		return nil, errorRegistry.New(ErrInvalidConfig).
			WithDetail("error", "dimension must be positive")
	}

	provider := &PgVectorProvider{
		schema:             DefaultSchema,
		tableName:          DefaultTableName,
		dimension:          dimension,
		maxConnections:     DefaultMaxConnections,
		connectionTimeout:  DefaultConnectionTimeout,
		defaultIndexType:   IndexTypeHNSW,
		defaultMetric:      DistanceCosine,
		autoCreateTable:    true,
		useNamespaceColumn: true,
		batchSize:          DefaultBatchSize,
		ownsConnection:     true, // We created the connection
	}

	// Apply options
	for _, opt := range opts {
		opt(provider)
	}

	// Connect to database
	ctx, cancel := context.WithTimeout(context.Background(), provider.connectionTimeout)
	defer cancel()

	dbx, err := ConnectSqlx(ctx, connStr, provider.maxConnections, provider.connectionTimeout)
	if err != nil {
		return nil, WrapError(err, ErrDatabaseConnection)
	}

	provider.db = dbx
	provider.client = NewDBClient(dbx, provider.schema, provider.tableName, provider.dimension, provider.useNamespaceColumn)

	// Ensure extension and table
	if err := provider.initialize(ctx); err != nil {
		dbx.Close()
		return nil, err
	}

	return provider, nil
}

// NewPgVectorProviderFromDB creates a new pgvector provider from an existing sqlx.DB connection
func NewPgVectorProviderFromDB(dbx *sqlx.DB, dimension int, opts ...ProviderOption) (*PgVectorProvider, *errx.Error) {
	if dbx == nil {
		return nil, errorRegistry.New(ErrMissingConfig).
			WithDetail("error", "database connection is required")
	}

	if dimension <= 0 {
		return nil, errorRegistry.New(ErrInvalidConfig).
			WithDetail("error", "dimension must be positive")
	}

	provider := &PgVectorProvider{
		schema:             DefaultSchema,
		tableName:          DefaultTableName,
		dimension:          dimension,
		defaultIndexType:   IndexTypeHNSW,
		defaultMetric:      DistanceCosine,
		autoCreateTable:    true,
		useNamespaceColumn: true,
		batchSize:          DefaultBatchSize,
		ownsConnection:     false, // Connection is owned by caller
	}

	// Apply options
	for _, opt := range opts {
		opt(provider)
	}

	provider.db = dbx
	provider.client = NewDBClient(dbx, provider.schema, provider.tableName, provider.dimension, provider.useNamespaceColumn)

	// Ensure extension and table
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := provider.initialize(ctx); err != nil {
		return nil, err
	}

	return provider, nil
}

// initialize sets up the database
func (p *PgVectorProvider) initialize(ctx context.Context) *errx.Error {
	// Test connection
	if err := p.db.PingContext(ctx); err != nil {
		return WrapError(err, ErrDatabaseConnection).
			WithDetail("error", "failed to ping database")
	}

	// Ensure pgvector extension
	if err := p.client.EnsureExtension(ctx); err != nil {
		return ParseDatabaseError(err, "CREATE EXTENSION IF NOT EXISTS vector")
	}

	// Create table if needed
	if p.autoCreateTable {
		if err := p.client.CreateTable(ctx); err != nil {
			return ParseDatabaseError(err, "CREATE TABLE")
		}
	}

	return nil
}

// Close closes the database connection (only if we own it)
func (p *PgVectorProvider) Close() error {
	if p.ownsConnection && p.db != nil {
		return p.db.Close()
	}
	// If we don't own the connection, don't close it
	return nil
}

// DB returns the underlying sqlx.DB connection
func (p *PgVectorProvider) DB() *sqlx.DB {
	return p.db
}

// ============================================================================
// VectorStorer Implementation
// ============================================================================

// Upsert inserts or updates vectors
func (p *PgVectorProvider) Upsert(ctx context.Context, vectors []vstore.Vector, opts ...vstore.Option) *errx.Error {
	if len(vectors) == 0 {
		return nil
	}

	// Validate vectors
	if err := ValidateVectors(vectors, p.dimension); err != nil {
		return err
	}

	options := vstore.ApplyOptions(opts...)
	namespace := options.Namespace

	// Build upsert query
	var query string
	if p.useNamespaceColumn {
		query = fmt.Sprintf(`
			INSERT INTO %s (id, vector, metadata, namespace, updated_at)
			VALUES ($1, $2, $3, $4, NOW())
			ON CONFLICT (id) DO UPDATE SET
				vector = EXCLUDED.vector,
				metadata = EXCLUDED.metadata,
				namespace = EXCLUDED.namespace,
				updated_at = NOW()`,
			p.client.FullTableName())
	} else {
		query = fmt.Sprintf(`
			INSERT INTO %s (id, vector, metadata, updated_at)
			VALUES ($1, $2, $3, NOW())
			ON CONFLICT (id) DO UPDATE SET
				vector = EXCLUDED.vector,
				metadata = EXCLUDED.metadata,
				updated_at = NOW()`,
			p.client.FullTableName())
	}

	// Execute in transaction for batch consistency
	tx, err := p.db.BeginTxx(ctx, nil)
	if err != nil {
		return ParseDatabaseError(err, "BEGIN TRANSACTION")
	}
	defer tx.Rollback()

	stmt, err := tx.PreparexContext(ctx, query)
	if err != nil {
		return ParseDatabaseError(err, query)
	}
	defer stmt.Close()

	for _, v := range vectors {
		pgVector := Vector(v.Values)
		metadata := Metadata(v.Metadata)

		var execErr error
		if p.useNamespaceColumn {
			_, execErr = stmt.ExecContext(ctx, v.ID, pgVector, metadata, namespace)
		} else {
			_, execErr = stmt.ExecContext(ctx, v.ID, pgVector, metadata)
		}

		if execErr != nil {
			return ParseDatabaseError(execErr, query, v.ID)
		}
	}

	if err := tx.Commit(); err != nil {
		return ParseDatabaseError(err, "COMMIT TRANSACTION")
	}

	return nil
}

// Query performs similarity search
func (p *PgVectorProvider) Query(ctx context.Context, vector []float32, opts ...vstore.Option) (*vstore.QueryResult, *errx.Error) {
	if len(vector) != p.dimension {
		return nil, errorRegistry.New(ErrInvalidVectorDimension).
			WithDetail("expected", p.dimension).
			WithDetail("got", len(vector))
	}

	options := vstore.ApplyOptions(opts...)

	// Build query
	selectFields := "id"
	if options.IncludeValues {
		selectFields += ", vector"
	}
	if options.IncludeMetadata {
		selectFields += ", metadata"
	}

	// Distance operator based on metric
	distanceOp := GetDistanceOperator(p.defaultMetric)

	query := fmt.Sprintf(`
		SELECT %s, vector %s $1 AS distance
		FROM %s`,
		selectFields, distanceOp, p.client.FullTableName())

	// Add namespace filter
	args := []any{Vector(vector)}
	argNum := 2
	if p.useNamespaceColumn && options.Namespace != "" {
		query += fmt.Sprintf(" WHERE namespace = $%d", argNum)
		args = append(args, options.Namespace)
		argNum++
	}

	// Add metadata filter if provided
	if options.Filter != nil {
		filterClause, filterArgs := p.buildFilterClause(options.Filter, argNum)
		if filterClause != "" {
			if strings.Contains(query, "WHERE") {
				query += " AND " + filterClause
			} else {
				query += " WHERE " + filterClause
			}
			args = append(args, filterArgs...)
		}
	}

	// Order by distance and limit
	query += fmt.Sprintf(" ORDER BY distance LIMIT %d", options.TopK)

	// Execute query using sqlx
	rows, err := p.db.QueryxContext(ctx, query, args...)
	if err != nil {
		return nil, ParseDatabaseError(err, query, args...)
	}
	defer rows.Close()

	// Use converter to build results
	builder := NewQueryResultBuilder(
		options.Namespace,
		options.IncludeValues,
		options.IncludeMetadata,
		p.defaultMetric,
	)

	for rows.Next() {
		if err := builder.ScanRow(rows.Rows); err != nil {
			return nil, ParseDatabaseError(err, "scanning row")
		}
	}

	if err := rows.Err(); err != nil {
		return nil, ParseDatabaseError(err, "iterating rows")
	}

	// Apply minimum score filter
	builder.FilterByMinScore(options.MinScore)

	return builder.Build(), nil
}

// Delete removes vectors by IDs
func (p *PgVectorProvider) Delete(ctx context.Context, ids []string, opts ...vstore.Option) *errx.Error {
	if len(ids) == 0 {
		return nil
	}

	options := vstore.ApplyOptions(opts...)

	query := fmt.Sprintf(`DELETE FROM %s WHERE id = ANY($1)`, p.client.FullTableName())
	args := []any{ids}

	if p.useNamespaceColumn && options.Namespace != "" {
		query += " AND namespace = $2"
		args = append(args, options.Namespace)
	}

	_, err := p.db.ExecContext(ctx, query, args...)
	if err != nil {
		return ParseDatabaseError(err, query, args...)
	}

	return nil
}

// Fetch retrieves vectors by IDs
func (p *PgVectorProvider) Fetch(ctx context.Context, ids []string, opts ...vstore.Option) ([]vstore.Vector, *errx.Error) {
	if len(ids) == 0 {
		return []vstore.Vector{}, nil
	}

	options := vstore.ApplyOptions(opts...)

	query := fmt.Sprintf(`
		SELECT id, vector, metadata
		FROM %s
		WHERE id = ANY($1)`,
		p.client.FullTableName())

	args := []any{ids}

	if p.useNamespaceColumn && options.Namespace != "" {
		query += " AND namespace = $2"
		args = append(args, options.Namespace)
	}

	rows, err := p.db.QueryxContext(ctx, query, args...)
	if err != nil {
		return nil, ParseDatabaseError(err, query, args...)
	}
	defer rows.Close()

	vectors := make([]vstore.Vector, 0, len(ids))
	for rows.Next() {
		var id string
		var pgVector Vector
		var metadata Metadata

		if err := rows.Scan(&id, &pgVector, &metadata); err != nil {
			return nil, ParseDatabaseError(err, "scanning row")
		}

		vectors = append(vectors, ToVstoreVector(id, pgVector, metadata))
	}

	return vectors, nil
}

// ============================================================================
// MetadataFilterer Implementation
// ============================================================================

// QueryWithFilter performs filtered similarity search
func (p *PgVectorProvider) QueryWithFilter(ctx context.Context, vector []float32, filter vstore.Filter, opts ...vstore.Option) (*vstore.QueryResult, *errx.Error) {
	// Add filter to options and use regular Query
	opts = append(opts, vstore.WithFilter(&filter))
	return p.Query(ctx, vector, opts...)
}

// ============================================================================
// BatchProcessor Implementation
// ============================================================================

// UpsertBatch upserts vectors in optimized batches
func (p *PgVectorProvider) UpsertBatch(ctx context.Context, vectors []vstore.Vector, opts ...vstore.Option) (*vstore.BatchResult, *errx.Error) {
	result := &vstore.BatchResult{}

	// Split into batches
	batches := SplitIntoBatches(vectors, p.batchSize)

	for _, batch := range batches {
		if err := p.Upsert(ctx, batch, opts...); err != nil {
			result.FailedCount += len(batch)
			for _, v := range batch {
				result.Errors = append(result.Errors, vstore.BatchError{
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
func (p *PgVectorProvider) DeleteBatch(ctx context.Context, ids []string, opts ...vstore.Option) (*vstore.BatchResult, *errx.Error) {
	if err := p.Delete(ctx, ids, opts...); err != nil {
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
func (p *PgVectorProvider) ListNamespaces(ctx context.Context) ([]string, *errx.Error) {
	if !p.useNamespaceColumn {
		return nil, errorRegistry.New(ErrFeatureNotSupported).
			WithDetail("error", "namespace column not enabled")
	}

	query := fmt.Sprintf(`
		SELECT DISTINCT namespace
		FROM %s
		ORDER BY namespace`,
		p.client.FullTableName())

	var namespaces []string
	if err := p.db.SelectContext(ctx, &namespaces, query); err != nil {
		return nil, ParseDatabaseError(err, query)
	}

	return namespaces, nil
}

// CreateNamespace creates a new namespace (no-op for column-based namespaces)
func (p *PgVectorProvider) CreateNamespace(ctx context.Context, namespace string) *errx.Error {
	// For column-based namespaces, this is a no-op
	// Namespaces are created implicitly when vectors are inserted
	return nil
}

// DeleteNamespace deletes a namespace and all its vectors
func (p *PgVectorProvider) DeleteNamespace(ctx context.Context, namespace string) *errx.Error {
	if !p.useNamespaceColumn {
		return errorRegistry.New(ErrFeatureNotSupported).
			WithDetail("error", "namespace column not enabled")
	}

	query := fmt.Sprintf(`DELETE FROM %s WHERE namespace = $1`, p.client.FullTableName())

	_, err := p.db.ExecContext(ctx, query, namespace)
	if err != nil {
		return ParseDatabaseError(err, query, namespace)
	}

	return nil
}

// ============================================================================
// IndexManager Implementation
// ============================================================================

// CreateIndex creates a vector index
func (p *PgVectorProvider) CreateIndex(ctx context.Context, config vstore.IndexConfig) *errx.Error {
	indexConfig := IndexConfig{
		IndexName:      config.Name,
		TableName:      p.tableName,
		IndexType:      p.defaultIndexType,
		DistanceMetric: VstoreMetricToPg(config.Metric),
	}

	if indexConfig.IndexName == "" {
		indexConfig.IndexName = fmt.Sprintf("idx_%s_vector", p.tableName)
	}

	if err := p.client.CreateVectorIndex(ctx, indexConfig); err != nil {
		return WrapError(err, ErrIndexCreationFailed)
	}

	return nil
}

// DeleteIndex deletes an index
func (p *PgVectorProvider) DeleteIndex(ctx context.Context, indexName string) *errx.Error {
	query := fmt.Sprintf(`DROP INDEX IF EXISTS %s.%s`, p.schema, indexName)

	_, err := p.db.ExecContext(ctx, query)
	if err != nil {
		return ParseDatabaseError(err, query)
	}

	return nil
}

// DescribeIndex returns index metadata
func (p *PgVectorProvider) DescribeIndex(ctx context.Context, indexName string) (*vstore.IndexInfo, *errx.Error) {
	tableInfo, err := p.client.GetTableInfo(ctx)
	if err != nil {
		return nil, WrapError(err, ErrTableNotFound)
	}

	// Find the specific index
	for _, idx := range tableInfo.Indexes {
		if idx.IndexName == indexName {
			return &vstore.IndexInfo{
				Name:             idx.IndexName,
				Dimension:        p.dimension,
				TotalVectorCount: tableInfo.VectorCount,
				Status:           "ready",
				Metadata: map[string]any{
					"index_type": idx.IndexType,
					"is_valid":   idx.IsValid,
				},
			}, nil
		}
	}

	return nil, errorRegistry.New(ErrIndexNotFound).
		WithDetail("index_name", indexName)
}

// ListIndexes returns all indexes
func (p *PgVectorProvider) ListIndexes(ctx context.Context) ([]vstore.IndexInfo, *errx.Error) {
	tableInfo, err := p.client.GetTableInfo(ctx)
	if err != nil {
		return nil, WrapError(err, ErrTableNotFound)
	}

	return ToVstoreIndexInfo(tableInfo), nil
}

// ============================================================================
// StatisticsProvider Implementation
// ============================================================================

// GetStatistics returns vector store statistics
func (p *PgVectorProvider) GetStatistics(ctx context.Context, opts ...vstore.Option) (*vstore.Statistics, *errx.Error) {
	options := vstore.ApplyOptions(opts...)

	var totalCount int64

	// Get total vector count
	var countQuery string
	var args []any

	if p.useNamespaceColumn && options.Namespace != "" {
		countQuery = fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE namespace = $1`, p.client.FullTableName())
		args = []any{options.Namespace}
	} else {
		countQuery = fmt.Sprintf(`SELECT COUNT(*) FROM %s`, p.client.FullTableName())
	}

	if err := p.db.GetContext(ctx, &totalCount, countQuery, args...); err != nil {
		return nil, ParseDatabaseError(err, countQuery, args...)
	}

	// Get namespace stats if enabled
	var namespaceStats []NamespaceStats
	if p.useNamespaceColumn {
		nsQuery := fmt.Sprintf(`
			SELECT namespace as name, COUNT(*) as vector_count
			FROM %s
			GROUP BY namespace`,
			p.client.FullTableName())

		if err := p.db.SelectContext(ctx, &namespaceStats, nsQuery); err != nil {
			return nil, ParseDatabaseError(err, nsQuery)
		}
	}

	return ToVstoreStatistics(totalCount, p.dimension, namespaceStats), nil
}

// ============================================================================
// Helper Methods
// ============================================================================

// buildFilterClause builds a WHERE clause from filter
func (p *PgVectorProvider) buildFilterClause(filter *vstore.Filter, startArgNum int) (string, []any) {
	if filter == nil {
		return "", nil
	}

	var clauses []string
	var args []any
	argNum := startArgNum

	// Process Must conditions (AND)
	for _, cond := range filter.Must {
		clause, condArgs := p.buildCondition(cond, argNum)
		if clause != "" {
			clauses = append(clauses, clause)
			args = append(args, condArgs...)
			argNum += len(condArgs)
		}
	}

	// Process Should conditions (OR)
	if len(filter.Should) > 0 {
		var shouldClauses []string
		for _, cond := range filter.Should {
			clause, condArgs := p.buildCondition(cond, argNum)
			if clause != "" {
				shouldClauses = append(shouldClauses, clause)
				args = append(args, condArgs...)
				argNum += len(condArgs)
			}
		}
		if len(shouldClauses) > 0 {
			clauses = append(clauses, "("+strings.Join(shouldClauses, " OR ")+")")
		}
	}

	// Process MustNot conditions (NOT)
	for _, cond := range filter.MustNot {
		clause, condArgs := p.buildCondition(cond, argNum)
		if clause != "" {
			clauses = append(clauses, "NOT ("+clause+")")
			args = append(args, condArgs...)
			argNum += len(condArgs)
		}
	}

	if len(clauses) == 0 {
		return "", nil
	}

	return strings.Join(clauses, " AND "), args
}

// buildCondition builds a single condition
func (p *PgVectorProvider) buildCondition(cond vstore.Condition, argNum int) (string, []any) {
	field := fmt.Sprintf("metadata->>'%s'", cond.Field)

	switch cond.Operator {
	case vstore.OpEqual:
		return fmt.Sprintf("%s = $%d", field, argNum), []any{cond.Value}
	case vstore.OpNotEqual:
		return fmt.Sprintf("%s != $%d", field, argNum), []any{cond.Value}
	case vstore.OpGreaterThan:
		return fmt.Sprintf("%s > $%d", field, argNum), []any{cond.Value}
	case vstore.OpLessThan:
		return fmt.Sprintf("%s < $%d", field, argNum), []any{cond.Value}
	case vstore.OpGreaterThanOrEqual:
		return fmt.Sprintf("%s >= $%d", field, argNum), []any{cond.Value}
	case vstore.OpLessThanOrEqual:
		return fmt.Sprintf("%s <= $%d", field, argNum), []any{cond.Value}
	case vstore.OpExists:
		return fmt.Sprintf("metadata ? '%s'", cond.Field), nil
	case vstore.OpContains:
		return fmt.Sprintf("%s LIKE $%d", field, argNum), []any{fmt.Sprintf("%%%v%%", cond.Value)}
	default:
		return "", nil
	}
}
