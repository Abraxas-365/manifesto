package fsx

import (
	"context"
	"io"
	"time"
)

// FileInfo represents information about a file
type FileInfo struct {
	Name        string            // Base name of the file
	Size        int64             // File size in bytes
	ModTime     time.Time         // Modification time
	IsDir       bool              // Is a directory
	ContentType string            // MIME type (when available)
	Metadata    map[string]string // Additional metadata
}

// FileReader provides read-only operations
type FileReader interface {
	ReadFile(ctx context.Context, path string) ([]byte, error)
	ReadFileStream(ctx context.Context, path string) (io.ReadCloser, error)
	Stat(ctx context.Context, path string) (FileInfo, error)
	List(ctx context.Context, path string) ([]FileInfo, error)
	Exists(ctx context.Context, path string) (bool, error)
}

// FileWriter provides write operations
type FileWriter interface {
	WriteFile(ctx context.Context, path string, data []byte) error
	WriteFileStream(ctx context.Context, path string, r io.Reader) error
	CreateDir(ctx context.Context, path string) error
}

// FileDeleter provides deletion operations
type FileDeleter interface {
	DeleteFile(ctx context.Context, path string) error
	DeleteDir(ctx context.Context, path string, recursive bool) error
}

// PathOperations provides path manipulation functionality
type PathOperations interface {
	Join(elem ...string) string
}

// PresignedURLOptions contains options for presigned URL generation
type PresignedURLOptions struct {
	Expiration  time.Duration     // How long the URL is valid
	ContentType string            // Content-Type header (for uploads)
	Metadata    map[string]string // Custom metadata (for uploads)
}

// PresignedURLGenerator provides presigned URL generation
type PresignedURLGenerator interface {
	// GetPresignedDownloadURL generates a presigned URL for downloading a file
	GetPresignedDownloadURL(ctx context.Context, path string, expiration time.Duration) (string, error)

	// GetPresignedUploadURL generates a presigned URL for uploading a file
	GetPresignedUploadURL(ctx context.Context, path string, expiration time.Duration) (string, error)

	// GetPresignedUploadURLWithOptions generates a presigned URL with additional options
	GetPresignedUploadURLWithOptions(ctx context.Context, path string, opts PresignedURLOptions) (string, error)
}

// FileSystem combines all file operations
type FileSystem interface {
	FileReader
	FileWriter
	FileDeleter
	PathOperations
}

// PathReader combines read and path operations
type PathReader interface {
	FileReader
	PathOperations
}

// FileSystemWithPresign combines standard file operations with presigned URL generation
type FileSystemWithPresign interface {
	FileSystem
	PresignedURLGenerator
}
