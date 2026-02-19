package vstpgvector

import (
	"context"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq" // PostgreSQL driver
)

// DBClient handles all database operations
type DBClient struct {
	db                 *sqlx.DB
	schema             string
	tableName          string
	dimension          int
	useNamespaceColumn bool
}

// NewDBClient creates a new database client
func NewDBClient(db *sqlx.DB, schema, tableName string, dimension int, useNamespaceColumn bool) *DBClient {
	if schema == "" {
		schema = "public"
	}

	return &DBClient{
		db:                 db,
		schema:             schema,
		tableName:          tableName,
		dimension:          dimension,
		useNamespaceColumn: useNamespaceColumn,
	}
}

// ConnectSqlx establishes a sqlx database connection
func ConnectSqlx(ctx context.Context, connStr string, maxConns int, timeout time.Duration) (*sqlx.DB, error) {
	dbx, err := sqlx.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings
	dbx.SetMaxOpenConns(maxConns)
	dbx.SetMaxIdleConns(maxConns / 2)
	dbx.SetConnMaxLifetime(time.Hour)

	// Test connection
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := dbx.PingContext(ctx); err != nil {
		dbx.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return dbx, nil
}

// EnsureExtension ensures pgvector extension is installed
func (c *DBClient) EnsureExtension(ctx context.Context) error {
	query := `CREATE EXTENSION IF NOT EXISTS vector`

	_, err := c.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create vector extension: %w", err)
	}

	return nil
}

// CreateTable creates the vectors table
func (c *DBClient) CreateTable(ctx context.Context) error {
	var query string

	if c.useNamespaceColumn {
		query = fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %s.%s (
				id TEXT PRIMARY KEY,
				vector vector(%d) NOT NULL,
				metadata JSONB,
				namespace TEXT DEFAULT '',
				created_at TIMESTAMP DEFAULT NOW(),
				updated_at TIMESTAMP DEFAULT NOW()
			)`,
			c.schema, c.tableName, c.dimension)
	} else {
		query = fmt.Sprintf(`
			CREATE TABLE IF NOT EXISTS %s.%s (
				id TEXT PRIMARY KEY,
				vector vector(%d) NOT NULL,
				metadata JSONB,
				created_at TIMESTAMP DEFAULT NOW(),
				updated_at TIMESTAMP DEFAULT NOW()
			)`,
			c.schema, c.tableName, c.dimension)
	}

	_, err := c.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	// Create indexes
	if c.useNamespaceColumn {
		indexQuery := fmt.Sprintf(`
			CREATE INDEX IF NOT EXISTS idx_%s_namespace 
			ON %s.%s (namespace)`,
			c.tableName, c.schema, c.tableName)

		if _, err := c.db.ExecContext(ctx, indexQuery); err != nil {
			return fmt.Errorf("failed to create namespace index: %w", err)
		}
	}

	// Create GIN index on metadata for filtering
	metadataIndexQuery := fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS idx_%s_metadata 
		ON %s.%s USING GIN (metadata)`,
		c.tableName, c.schema, c.tableName)

	if _, err := c.db.ExecContext(ctx, metadataIndexQuery); err != nil {
		return fmt.Errorf("failed to create metadata index: %w", err)
	}

	return nil
}

// CreateVectorIndex creates a vector similarity index
func (c *DBClient) CreateVectorIndex(ctx context.Context, config IndexConfig) error {
	var query string

	switch config.IndexType {
	case IndexTypeIVFFlat:
		lists := config.Lists
		if lists == 0 {
			lists = 100 // Default
		}
		query = fmt.Sprintf(`
			CREATE INDEX IF NOT EXISTS %s 
			ON %s.%s 
			USING ivfflat (vector %s) 
			WITH (lists = %d)`,
			config.IndexName, c.schema, c.tableName,
			config.DistanceMetric, lists)

	case IndexTypeHNSW:
		m := config.M
		if m == 0 {
			m = 16 // Default
		}
		efConstruction := config.EfConstruction
		if efConstruction == 0 {
			efConstruction = 64 // Default
		}
		query = fmt.Sprintf(`
			CREATE INDEX IF NOT EXISTS %s 
			ON %s.%s 
			USING hnsw (vector %s) 
			WITH (m = %d, ef_construction = %d)`,
			config.IndexName, c.schema, c.tableName,
			config.DistanceMetric, m, efConstruction)

	default:
		return fmt.Errorf("unsupported index type: %s", config.IndexType)
	}

	_, err := c.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create vector index: %w", err)
	}

	return nil
}

// FullTableName returns the fully qualified table name
func (c *DBClient) FullTableName() string {
	return fmt.Sprintf("%s.%s", c.schema, c.tableName)
}

// Close closes the database connection
func (c *DBClient) Close() error {
	return c.db.Close()
}

// TableExists checks if the table exists
func (c *DBClient) TableExists(ctx context.Context) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_schema = $1 
			AND table_name = $2
		)`

	var exists bool
	err := c.db.GetContext(ctx, &exists, query, c.schema, c.tableName)
	if err != nil {
		return false, err
	}

	return exists, nil
}

// GetTableInfo retrieves table metadata
func (c *DBClient) GetTableInfo(ctx context.Context) (*TableInfo, error) {
	info := &TableInfo{
		TableName: c.tableName,
		Schema:    c.schema,
		Dimension: c.dimension,
	}

	// Get vector count
	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM %s`, c.FullTableName())
	if err := c.db.GetContext(ctx, &info.VectorCount, countQuery); err != nil {
		return nil, err
	}

	// Get indexes
	indexQuery := `
		SELECT 
			indexname,
			indexdef
		FROM pg_indexes
		WHERE schemaname = $1 AND tablename = $2`

	type indexRow struct {
		IndexName string `db:"indexname"`
		IndexDef  string `db:"indexdef"`
	}

	var rows []indexRow
	if err := c.db.SelectContext(ctx, &rows, indexQuery, c.schema, c.tableName); err != nil {
		return nil, err
	}

	info.Indexes = make([]IndexInfo, 0, len(rows))
	for _, row := range rows {
		indexInfo := IndexInfo{
			IndexName: row.IndexName,
			IsValid:   true,
			IndexType: DetermineIndexTypeFromDef(row.IndexDef),
		}
		info.Indexes = append(info.Indexes, indexInfo)
	}

	return info, nil
}
