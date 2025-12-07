package fsxlocal

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/Abraxas-365/manifesto/pkg/fsx"
)

// LocalFileSystem implements fsx.FileSystem using local disk
type LocalFileSystem struct {
	basePath string // Root directory for all files
}

// NewLocalFileSystem creates a new local file system
// basePath: root directory (e.g., "./uploads" or "/tmp/manifesto-files")
func NewLocalFileSystem(basePath string) (*LocalFileSystem, error) {
	// Create base directory if it doesn't exist
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create base directory: %w", err)
	}

	absPath, err := filepath.Abs(basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute path: %w", err)
	}

	return &LocalFileSystem{
		basePath: absPath,
	}, nil
}

// ============================================================================
// FileReader Implementation
// ============================================================================

func (fs *LocalFileSystem) ReadFile(ctx context.Context, path string) ([]byte, error) {
	fullPath := fs.fullPath(path)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %s", path)
		}
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	return data, nil
}

func (fs *LocalFileSystem) ReadFileStream(ctx context.Context, path string) (io.ReadCloser, error) {
	fullPath := fs.fullPath(path)
	file, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %s", path)
		}
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	return file, nil
}

func (fs *LocalFileSystem) Stat(ctx context.Context, path string) (fsx.FileInfo, error) {
	fullPath := fs.fullPath(path)
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fsx.FileInfo{}, fmt.Errorf("file not found: %s", path)
		}
		return fsx.FileInfo{}, fmt.Errorf("failed to stat file: %w", err)
	}

	return fsx.FileInfo{
		Name:        info.Name(),
		Size:        info.Size(),
		ModTime:     info.ModTime(),
		IsDir:       info.IsDir(),
		ContentType: detectContentType(fullPath),
		Metadata:    make(map[string]string),
	}, nil
}

func (fs *LocalFileSystem) List(ctx context.Context, path string) ([]fsx.FileInfo, error) {
	fullPath := fs.fullPath(path)
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("directory not found: %s", path)
		}
		return nil, fmt.Errorf("failed to list directory: %w", err)
	}

	fileInfos := make([]fsx.FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue // Skip files with errors
		}

		fileInfos = append(fileInfos, fsx.FileInfo{
			Name:        info.Name(),
			Size:        info.Size(),
			ModTime:     info.ModTime(),
			IsDir:       info.IsDir(),
			ContentType: detectContentType(filepath.Join(fullPath, info.Name())),
			Metadata:    make(map[string]string),
		})
	}

	return fileInfos, nil
}

func (fs *LocalFileSystem) Exists(ctx context.Context, path string) (bool, error) {
	fullPath := fs.fullPath(path)
	_, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ============================================================================
// FileWriter Implementation
// ============================================================================

func (fs *LocalFileSystem) WriteFile(ctx context.Context, path string, data []byte) error {
	fullPath := fs.fullPath(path)

	// Create parent directories
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	// Write file
	if err := os.WriteFile(fullPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func (fs *LocalFileSystem) WriteFileStream(ctx context.Context, path string, r io.Reader) error {
	fullPath := fs.fullPath(path)

	// Create parent directories
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	// Create file
	file, err := os.Create(fullPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Copy data
	if _, err := io.Copy(file, r); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func (fs *LocalFileSystem) CreateDir(ctx context.Context, path string) error {
	fullPath := fs.fullPath(path)
	if err := os.MkdirAll(fullPath, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	return nil
}

// ============================================================================
// FileDeleter Implementation
// ============================================================================

func (fs *LocalFileSystem) DeleteFile(ctx context.Context, path string) error {
	fullPath := fs.fullPath(path)
	if err := os.Remove(fullPath); err != nil {
		if os.IsNotExist(err) {
			return nil // Already deleted
		}
		return fmt.Errorf("failed to delete file: %w", err)
	}
	return nil
}

func (fs *LocalFileSystem) DeleteDir(ctx context.Context, path string, recursive bool) error {
	fullPath := fs.fullPath(path)

	if recursive {
		if err := os.RemoveAll(fullPath); err != nil {
			return fmt.Errorf("failed to delete directory: %w", err)
		}
	} else {
		if err := os.Remove(fullPath); err != nil {
			return fmt.Errorf("failed to delete directory: %w", err)
		}
	}

	return nil
}

// ============================================================================
// PathOperations Implementation
// ============================================================================

func (fs *LocalFileSystem) Join(elem ...string) string {
	return filepath.Join(elem...)
}

// ============================================================================
// Helper Methods
// ============================================================================

// fullPath converts a relative path to absolute path
func (fs *LocalFileSystem) fullPath(path string) string {
	return filepath.Join(fs.basePath, path)
}

// detectContentType detects MIME type from file extension
func detectContentType(path string) string {
	ext := filepath.Ext(path)
	switch ext {
	case ".pdf":
		return "application/pdf"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".txt":
		return "text/plain"
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".zip":
		return "application/zip"
	default:
		return "application/octet-stream"
	}
}

// GetBasePath returns the base path
func (fs *LocalFileSystem) GetBasePath() string {
	return fs.basePath
}
