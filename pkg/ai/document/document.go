package document

import (
	"context"
	"io"
)

// Document represents a piece of content with metadata
type Document struct {
	// Core fields
	ID      string
	Content string

	// Metadata about the document
	Metadata Metadata

	// Optional: Pre-computed embedding
	Embedding []float32

	// Optional: Sparse embedding for hybrid search
	SparseEmbedding map[uint32]float32
}

// Metadata contains document metadata
type Metadata map[string]any

// Common metadata keys
const (
	MetadataSource       = "source"        // Source file/URL
	MetadataTitle        = "title"         // Document title
	MetadataAuthor       = "author"        // Author
	MetadataCreatedAt    = "created_at"    // Creation timestamp
	MetadataUpdatedAt    = "updated_at"    // Update timestamp
	MetadataPageNumber   = "page_number"   // Page number
	MetadataChunkIndex   = "chunk_index"   // Chunk index
	MetadataChunkTotal   = "chunk_total"   // Total chunks
	MetadataDocumentID   = "document_id"   // Parent document ID
	MetadataDocumentType = "document_type" // Type: pdf, markdown, etc.
	MetadataLanguage     = "language"      // Language code
	MetadataCategory     = "category"      // Category/topic
	MetadataTags         = "tags"          // Tags array
	MetadataUserID       = "user_id"       // Owner user ID
	MetadataFileSize     = "file_size"     // File size in bytes
)

// NewDocument creates a new document
func NewDocument(content string) *Document {
	return &Document{
		Content:  content,
		Metadata: make(Metadata),
	}
}

// WithID sets the document ID
func (d *Document) WithID(id string) *Document {
	d.ID = id
	return d
}

// WithMetadata adds metadata
func (d *Document) WithMetadata(key string, value any) *Document {
	d.Metadata[key] = value
	return d
}

// WithMetadataMap adds multiple metadata fields
func (d *Document) WithMetadataMap(metadata map[string]any) *Document {
	for k, v := range metadata {
		d.Metadata[k] = v
	}
	return d
}

// WithEmbedding sets the embedding
func (d *Document) WithEmbedding(embedding []float32) *Document {
	d.Embedding = embedding
	return d
}

// Clone creates a deep copy of the document
func (d *Document) Clone() *Document {
	clone := &Document{
		ID:       d.ID,
		Content:  d.Content,
		Metadata: make(Metadata, len(d.Metadata)),
	}

	for k, v := range d.Metadata {
		clone.Metadata[k] = v
	}

	if d.Embedding != nil {
		clone.Embedding = make([]float32, len(d.Embedding))
		copy(clone.Embedding, d.Embedding)
	}

	return clone
}

// GetMetadataString safely retrieves a string metadata field
func (d *Document) GetMetadataString(key string) (string, bool) {
	if val, ok := d.Metadata[key]; ok {
		if str, ok := val.(string); ok {
			return str, true
		}
	}
	return "", false
}

// GetMetadataInt safely retrieves an int metadata field
func (d *Document) GetMetadataInt(key string) (int, bool) {
	if val, ok := d.Metadata[key]; ok {
		switch v := val.(type) {
		case int:
			return v, true
		case int64:
			return int(v), true
		case float64:
			return int(v), true
		}
	}
	return 0, false
}

// ============================================================================
// Document Stream - for memory-efficient processing
// ============================================================================

// DocumentStream represents a stream of documents
type DocumentStream interface {
	// Next returns the next document, or io.EOF when done
	Next() (*Document, error)

	// Close releases resources
	Close() error
}

// DocumentStreamFunc is an adapter to use functions as DocumentStream
type DocumentStreamFunc func() (*Document, error)

func (f DocumentStreamFunc) Next() (*Document, error) {
	return f()
}

func (f DocumentStreamFunc) Close() error {
	return nil
}

// ============================================================================
// Document Loader - loads documents from various sources
// ============================================================================

// Loader loads documents from various sources
type Loader interface {
	// Load loads all documents (use with caution for large files)
	Load(ctx context.Context) ([]*Document, error)

	// LoadStream loads documents as a stream
	LoadStream(ctx context.Context) (DocumentStream, error)
}

// ============================================================================
// Document Source - represents where documents come from
// ============================================================================

// Source represents a document source
type Source struct {
	Type     SourceType
	Path     string         // File path
	Reader   io.Reader      // Reader
	URL      string         // URL
	Data     []byte         // Raw data
	Metadata map[string]any // Additional metadata
}

type SourceType string

const (
	SourceTypeFile   SourceType = "file"
	SourceTypeReader SourceType = "reader"
	SourceTypeURL    SourceType = "url"
	SourceTypeBytes  SourceType = "bytes"
	SourceTypeString SourceType = "string"
)

// FromFile creates a source from a file path
func FromFile(path string) Source {
	return Source{
		Type: SourceTypeFile,
		Path: path,
	}
}

// FromReader creates a source from a reader
func FromReader(reader io.Reader) Source {
	return Source{
		Type:   SourceTypeReader,
		Reader: reader,
	}
}

// FromURL creates a source from a URL
func FromURL(url string) Source {
	return Source{
		Type: SourceTypeURL,
		URL:  url,
	}
}

// FromString creates a source from a string
func FromString(content string) Source {
	return Source{
		Type: SourceTypeString,
		Data: []byte(content),
	}
}

// ============================================================================
// Batch Operations
// ============================================================================

// Batch represents a batch of documents
type Batch struct {
	Documents []*Document
	Metadata  map[string]any
}

// NewBatch creates a new batch
func NewBatch(docs ...*Document) *Batch {
	return &Batch{
		Documents: docs,
		Metadata:  make(map[string]any),
	}
}

// Size returns the number of documents in the batch
func (b *Batch) Size() int {
	return len(b.Documents)
}

// ============================================================================
// Helper Types
// ============================================================================

// DocumentProcessor processes documents
type DocumentProcessor func(ctx context.Context, doc *Document) (*Document, error)

// DocumentFilter filters documents
type DocumentFilter func(doc *Document) bool

// DocumentTransformer transforms a stream of documents
type DocumentTransformer interface {
	Transform(ctx context.Context, stream DocumentStream) (DocumentStream, error)
}
