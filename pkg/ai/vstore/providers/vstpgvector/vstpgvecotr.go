package vstpgvector

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Vector represents a pgvector vector type
type Vector []float32

// Value implements driver.Valuer for Vector
func (v Vector) Value() (driver.Value, error) {
	if v == nil {
		return nil, nil
	}

	// Format as PostgreSQL vector literal: [1,2,3]
	parts := make([]string, len(v))
	for i, val := range v {
		parts[i] = fmt.Sprintf("%f", val)
	}
	return "[" + strings.Join(parts, ",") + "]", nil
}

// Scan implements sql.Scanner for Vector
func (v *Vector) Scan(src any) error {
	if src == nil {
		*v = nil
		return nil
	}

	switch src := src.(type) {
	case []byte:
		return v.scanString(string(src))
	case string:
		return v.scanString(src)
	default:
		return fmt.Errorf("unsupported type for Vector: %T", src)
	}
}

func (v *Vector) scanString(s string) error {
	// Parse PostgreSQL vector format: [1,2,3]
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return errors.New("invalid vector format")
	}

	s = s[1 : len(s)-1] // Remove brackets
	if s == "" {
		*v = Vector{}
		return nil
	}

	parts := strings.Split(s, ",")
	result := make(Vector, len(parts))

	for i, part := range parts {
		var val float32
		if _, err := fmt.Sscanf(strings.TrimSpace(part), "%f", &val); err != nil {
			return fmt.Errorf("invalid vector value at index %d: %w", i, err)
		}
		result[i] = val
	}

	*v = result
	return nil
}

// Metadata represents JSONB metadata
type Metadata map[string]any

// Value implements driver.Valuer for Metadata
func (m Metadata) Value() (driver.Value, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}

// Scan implements sql.Scanner for Metadata
func (m *Metadata) Scan(src any) error {
	if src == nil {
		*m = nil
		return nil
	}

	var data []byte
	switch src := src.(type) {
	case []byte:
		data = src
	case string:
		data = []byte(src)
	default:
		return fmt.Errorf("unsupported type for Metadata: %T", src)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return err
	}
	*m = result
	return nil
}

// VectorRecord represents a row in the vectors table
type VectorRecord struct {
	ID        string
	Vector    Vector
	Metadata  Metadata
	Namespace string
}

// IndexType represents the type of vector index
type IndexType string

const (
	IndexTypeIVFFlat IndexType = "ivfflat"
	IndexTypeHNSW    IndexType = "hnsw"
)

// DistanceMetric represents the distance function
type DistanceMetric string

const (
	DistanceL2           DistanceMetric = "vector_l2_ops"
	DistanceInnerProduct DistanceMetric = "vector_ip_ops"
	DistanceCosine       DistanceMetric = "vector_cosine_ops"
)

// TableConfig represents table configuration
type TableConfig struct {
	TableName string
	Dimension int
	Schema    string // PostgreSQL schema (default: "public")
}

// IndexConfig represents index configuration
type IndexConfig struct {
	IndexName      string
	TableName      string
	IndexType      IndexType
	DistanceMetric DistanceMetric
	Lists          int // For IVFFlat
	M              int // For HNSW
	EfConstruction int // For HNSW
}

// QueryParams represents query parameters
type QueryParams struct {
	Vector          Vector
	TopK            int
	Namespace       string
	IncludeValues   bool
	IncludeMetadata bool
	MinScore        float32
	Filter          map[string]any
}

// TableInfo represents table metadata
type TableInfo struct {
	TableName   string
	Schema      string
	VectorCount int64
	Dimension   int
	Indexes     []IndexInfo
}

// IndexInfo represents index metadata
type IndexInfo struct {
	IndexName string
	IndexType string
	Size      string
	IsValid   bool
}
