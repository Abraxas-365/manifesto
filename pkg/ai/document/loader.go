package document

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ============================================================================
// Text Loader - loads plain text files
// ============================================================================

// TextLoader loads plain text documents
type TextLoader struct {
	source   Source
	splitter Splitter
	metadata map[string]any
}

// NewTextLoader creates a new text loader
func NewTextLoader(source Source) *TextLoader {
	return &TextLoader{
		source:   source,
		metadata: make(map[string]any),
	}
}

// WithSplitter sets the splitter
func (l *TextLoader) WithSplitter(splitter Splitter) *TextLoader {
	l.splitter = splitter
	return l
}

// WithMetadata adds metadata
func (l *TextLoader) WithMetadata(key string, value any) *TextLoader {
	l.metadata[key] = value
	return l
}

// Load loads all documents into memory
func (l *TextLoader) Load(ctx context.Context) ([]*Document, error) {
	content, err := l.readContent()
	if err != nil {
		return nil, err
	}

	doc := NewDocument(content).
		WithMetadataMap(l.metadata).
		WithMetadata(MetadataSource, l.getSourceName())

	if l.splitter != nil {
		return l.splitter.Split(ctx, doc)
	}

	return []*Document{doc}, nil
}

// LoadStream loads documents as a stream (memory efficient)
func (l *TextLoader) LoadStream(ctx context.Context) (DocumentStream, error) {
	reader, err := l.getReader()
	if err != nil {
		return nil, err
	}

	// Return streaming implementation
	return &textStream{
		reader:   reader,
		scanner:  bufio.NewScanner(reader),
		metadata: l.metadata,
		source:   l.getSourceName(),
		splitter: l.splitter,
		ctx:      ctx,
	}, nil
}

func (l *TextLoader) readContent() (string, error) {
	reader, err := l.getReader()
	if err != nil {
		return "", err
	}
	defer func() {
		if closer, ok := reader.(io.Closer); ok {
			closer.Close()
		}
	}()

	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

func (l *TextLoader) getReader() (io.Reader, error) {
	switch l.source.Type {
	case SourceTypeFile:
		return os.Open(l.source.Path)
	case SourceTypeReader:
		return l.source.Reader, nil
	case SourceTypeBytes, SourceTypeString:
		return strings.NewReader(string(l.source.Data)), nil
	default:
		return nil, fmt.Errorf("unsupported source type: %s", l.source.Type)
	}
}

func (l *TextLoader) getSourceName() string {
	switch l.source.Type {
	case SourceTypeFile:
		return l.source.Path
	case SourceTypeURL:
		return l.source.URL
	default:
		return "unknown"
	}
}

// textStream implements DocumentStream for text files
type textStream struct {
	reader   io.Reader
	scanner  *bufio.Scanner
	metadata map[string]any
	source   string
	splitter Splitter
	ctx      context.Context

	buffer       []*Document
	bufferIndex  int
	chunkCounter int
	closed       bool
}

func (s *textStream) Next() (*Document, error) {
	if s.closed {
		return nil, io.EOF
	}

	// Return buffered chunks first
	if s.bufferIndex < len(s.buffer) {
		doc := s.buffer[s.bufferIndex]
		s.bufferIndex++
		return doc, nil
	}

	// Read next line/chunk
	if !s.scanner.Scan() {
		if err := s.scanner.Err(); err != nil {
			return nil, err
		}
		return nil, io.EOF
	}

	content := s.scanner.Text()
	if content == "" {
		return s.Next() // Skip empty lines
	}

	doc := NewDocument(content).
		WithID(fmt.Sprintf("%s_chunk_%d", s.source, s.chunkCounter)).
		WithMetadataMap(s.metadata).
		WithMetadata(MetadataSource, s.source).
		WithMetadata(MetadataChunkIndex, s.chunkCounter)

	s.chunkCounter++

	// Apply splitter if set
	if s.splitter != nil {
		chunks, err := s.splitter.Split(s.ctx, doc)
		if err != nil {
			return nil, err
		}

		if len(chunks) > 0 {
			s.buffer = chunks
			s.bufferIndex = 0
			return s.Next()
		}
	}

	return doc, nil
}

func (s *textStream) Close() error {
	s.closed = true
	if closer, ok := s.reader.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// ============================================================================
// Directory Loader - loads multiple files from a directory
// ============================================================================

// DirectoryLoader loads documents from a directory
type DirectoryLoader struct {
	path      string
	pattern   string // Glob pattern
	recursive bool
	splitter  Splitter
	metadata  map[string]any
}

// NewDirectoryLoader creates a new directory loader
func NewDirectoryLoader(path string) *DirectoryLoader {
	return &DirectoryLoader{
		path:     path,
		pattern:  "*",
		metadata: make(map[string]any),
	}
}

// WithPattern sets the file pattern
func (l *DirectoryLoader) WithPattern(pattern string) *DirectoryLoader {
	l.pattern = pattern
	return l
}

// WithRecursive enables recursive directory scanning
func (l *DirectoryLoader) WithRecursive(recursive bool) *DirectoryLoader {
	l.recursive = recursive
	return l
}

// WithSplitter sets the splitter
func (l *DirectoryLoader) WithSplitter(splitter Splitter) *DirectoryLoader {
	l.splitter = splitter
	return l
}

// Load loads all documents
func (l *DirectoryLoader) Load(ctx context.Context) ([]*Document, error) {
	files, err := l.listFiles()
	if err != nil {
		return nil, err
	}

	var allDocs []*Document
	for _, file := range files {
		loader := NewTextLoader(FromFile(file)).
			WithSplitter(l.splitter).
			WithMetadata(MetadataSource, file)

		docs, err := loader.Load(ctx)
		if err != nil {
			// Log error but continue
			continue
		}

		allDocs = append(allDocs, docs...)
	}

	return allDocs, nil
}

// LoadStream loads documents as a stream
func (l *DirectoryLoader) LoadStream(ctx context.Context) (DocumentStream, error) {
	files, err := l.listFiles()
	if err != nil {
		return nil, err
	}

	return &directoryStream{
		files:    files,
		splitter: l.splitter,
		ctx:      ctx,
	}, nil
}

func (l *DirectoryLoader) listFiles() ([]string, error) {
	// Simple implementation - list files in directory
	// In production, use filepath.Walk or filepath.Glob
	entries, err := os.ReadDir(l.path)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if !entry.IsDir() {
			files = append(files, filepath.Join(l.path, entry.Name()))
		}
	}

	return files, nil
}

// directoryStream implements DocumentStream for directories
type directoryStream struct {
	files       []string
	fileIndex   int
	currentFile DocumentStream
	splitter    Splitter
	ctx         context.Context
}

func (s *directoryStream) Next() (*Document, error) {
	// Try current file stream
	if s.currentFile != nil {
		doc, err := s.currentFile.Next()
		if err == nil {
			return doc, nil
		}
		if err != io.EOF {
			return nil, err
		}
		// EOF - close and move to next file
		s.currentFile.Close()
		s.currentFile = nil
	}

	// Move to next file
	if s.fileIndex >= len(s.files) {
		return nil, io.EOF
	}

	file := s.files[s.fileIndex]
	s.fileIndex++

	// Create loader for this file
	loader := NewTextLoader(FromFile(file)).WithSplitter(s.splitter)
	stream, err := loader.LoadStream(s.ctx)
	if err != nil {
		// Skip this file
		return s.Next()
	}

	s.currentFile = stream
	return s.Next()
}

func (s *directoryStream) Close() error {
	if s.currentFile != nil {
		return s.currentFile.Close()
	}
	return nil
}
