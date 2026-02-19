package vstpgvector

import (
	"time"
)

// ProviderOption configures the pgvector provider
type ProviderOption func(*PgVectorProvider)

// WithSchema sets the PostgreSQL schema
func WithSchema(schema string) ProviderOption {
	return func(p *PgVectorProvider) {
		p.schema = schema
	}
}

// WithTableName sets the table name for vectors
func WithTableName(tableName string) ProviderOption {
	return func(p *PgVectorProvider) {
		p.tableName = tableName
	}
}

// WithDimension sets the vector dimension
func WithDimension(dimension int) ProviderOption {
	return func(p *PgVectorProvider) {
		p.dimension = dimension
	}
}

// WithMaxConnections sets the maximum number of database connections
func WithMaxConnections(max int) ProviderOption {
	return func(p *PgVectorProvider) {
		p.maxConnections = max
	}
}

// WithConnectionTimeout sets the connection timeout
func WithConnectionTimeout(timeout time.Duration) ProviderOption {
	return func(p *PgVectorProvider) {
		p.connectionTimeout = timeout
	}
}

// WithIndexType sets the default index type
func WithIndexType(indexType IndexType) ProviderOption {
	return func(p *PgVectorProvider) {
		p.defaultIndexType = indexType
	}
}

// WithDistanceMetric sets the default distance metric
func WithDistanceMetric(metric DistanceMetric) ProviderOption {
	return func(p *PgVectorProvider) {
		p.defaultMetric = metric
	}
}

// WithAutoCreateTable automatically creates the table if it doesn't exist
func WithAutoCreateTable(auto bool) ProviderOption {
	return func(p *PgVectorProvider) {
		p.autoCreateTable = auto
	}
}

// WithNamespaceColumn enables namespace support via a column
func WithNamespaceColumn(enabled bool) ProviderOption {
	return func(p *PgVectorProvider) {
		p.useNamespaceColumn = enabled
	}
}

// WithBatchSize sets the default batch size for operations
func WithBatchSize(size int) ProviderOption {
	return func(p *PgVectorProvider) {
		p.batchSize = size
	}
}
