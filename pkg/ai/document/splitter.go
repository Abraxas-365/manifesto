package document

import (
	"context"
	"fmt"
	"strings"
)

// Splitter splits documents into chunks
type Splitter interface {
	// Split splits a document into multiple chunks
	Split(ctx context.Context, doc *Document) ([]*Document, error)

	// SplitStream splits documents from a stream
	SplitStream(ctx context.Context, stream DocumentStream) (DocumentStream, error)
}

// ============================================================================
// Text Splitter - splits by character count
// ============================================================================

// TextSplitter splits text into chunks
type TextSplitter struct {
	ChunkSize     int      // Target chunk size
	ChunkOverlap  int      // Overlap between chunks
	Separators    []string // Separators to try (in order)
	KeepSeparator bool     // Keep separator in chunks
}

// NewTextSplitter creates a new text splitter
func NewTextSplitter(chunkSize, chunkOverlap int) *TextSplitter {
	return &TextSplitter{
		ChunkSize:     chunkSize,
		ChunkOverlap:  chunkOverlap,
		Separators:    []string{"\n\n", "\n", " ", ""},
		KeepSeparator: false,
	}
}

// Split splits a document into chunks
func (s *TextSplitter) Split(ctx context.Context, doc *Document) ([]*Document, error) {
	if doc == nil || doc.Content == "" {
		return []*Document{}, nil
	}

	chunks := s.splitText(doc.Content)
	documents := make([]*Document, 0, len(chunks))

	for i, chunk := range chunks {
		newDoc := doc.Clone()
		newDoc.Content = chunk
		newDoc.ID = fmt.Sprintf("%s_chunk_%d", doc.ID, i)
		newDoc.Metadata[MetadataChunkIndex] = i
		newDoc.Metadata[MetadataChunkTotal] = len(chunks)
		newDoc.Metadata[MetadataDocumentID] = doc.ID

		documents = append(documents, newDoc)
	}

	return documents, nil
}

// SplitStream splits documents from a stream
func (s *TextSplitter) SplitStream(ctx context.Context, stream DocumentStream) (DocumentStream, error) {
	return &splitStream{
		source:   stream,
		splitter: s,
		ctx:      ctx,
	}, nil
}

// splitStream implements DocumentStream for splitting
type splitStream struct {
	source      DocumentStream
	splitter    *TextSplitter
	ctx         context.Context
	buffer      []*Document
	bufferIndex int
}

func (ss *splitStream) Next() (*Document, error) {
	// Return buffered documents first
	if ss.bufferIndex < len(ss.buffer) {
		doc := ss.buffer[ss.bufferIndex]
		ss.bufferIndex++
		return doc, nil
	}

	// Reset buffer
	ss.buffer = nil
	ss.bufferIndex = 0

	// Get next document from source stream
	doc, err := ss.source.Next()
	if err != nil {
		return nil, err
	}

	// Split the document
	chunks, err := ss.splitter.Split(ss.ctx, doc)
	if err != nil {
		return nil, err
	}

	if len(chunks) == 0 {
		// No chunks produced, try next document
		return ss.Next()
	}

	// Buffer the chunks
	ss.buffer = chunks
	ss.bufferIndex = 0

	// Return first chunk
	result := ss.buffer[0]
	ss.bufferIndex = 1
	return result, nil
}

func (ss *splitStream) Close() error {
	return ss.source.Close()
}

// splitText splits text into chunks
func (s *TextSplitter) splitText(text string) []string {
	if len(text) <= s.ChunkSize {
		return []string{text}
	}

	// Try each separator
	for _, separator := range s.Separators {
		if separator == "" {
			// Character-level splitting
			return s.splitByCharacter(text)
		}

		chunks := s.splitBySeparator(text, separator)
		if len(chunks) > 1 {
			return s.mergeChunks(chunks)
		}
	}

	// Fallback to character splitting
	return s.splitByCharacter(text)
}

// splitBySeparator splits text by a separator
func (s *TextSplitter) splitBySeparator(text, separator string) []string {
	if separator == "" {
		return []string{text}
	}

	parts := strings.Split(text, separator)
	if !s.KeepSeparator {
		return parts
	}

	// Keep separator with chunks
	result := make([]string, 0, len(parts))
	for i, part := range parts {
		if i < len(parts)-1 {
			result = append(result, part+separator)
		} else {
			result = append(result, part)
		}
	}
	return result
}

// mergeChunks merges small chunks up to chunk size
func (s *TextSplitter) mergeChunks(chunks []string) []string {
	if len(chunks) == 0 {
		return chunks
	}

	result := make([]string, 0)
	currentChunk := ""

	for _, chunk := range chunks {
		if chunk == "" {
			continue
		}

		// If adding this chunk would exceed size, save current and start new
		if len(currentChunk) > 0 && len(currentChunk)+len(chunk) > s.ChunkSize {
			result = append(result, strings.TrimSpace(currentChunk))

			// Add overlap from end of previous chunk
			if s.ChunkOverlap > 0 && len(currentChunk) > s.ChunkOverlap {
				overlap := currentChunk[len(currentChunk)-s.ChunkOverlap:]
				currentChunk = overlap + chunk
			} else {
				currentChunk = chunk
			}
		} else {
			if currentChunk != "" {
				currentChunk += chunk
			} else {
				currentChunk = chunk
			}
		}
	}

	// Add remaining chunk
	if currentChunk != "" {
		result = append(result, strings.TrimSpace(currentChunk))
	}

	return result
}

// splitByCharacter splits text character by character
func (s *TextSplitter) splitByCharacter(text string) []string {
	result := make([]string, 0)
	runes := []rune(text)

	for i := 0; i < len(runes); i += s.ChunkSize - s.ChunkOverlap {
		end := i + s.ChunkSize
		if end > len(runes) {
			end = len(runes)
		}
		result = append(result, string(runes[i:end]))
	}

	return result
}

// ============================================================================
// Recursive Character Text Splitter - tries multiple separators recursively
// ============================================================================

// RecursiveTextSplitter splits text recursively using different separators
type RecursiveTextSplitter struct {
	ChunkSize    int
	ChunkOverlap int
	Separators   []string
}

// NewRecursiveTextSplitter creates a new recursive text splitter
func NewRecursiveTextSplitter(chunkSize, chunkOverlap int) *RecursiveTextSplitter {
	return &RecursiveTextSplitter{
		ChunkSize:    chunkSize,
		ChunkOverlap: chunkOverlap,
		Separators: []string{
			"\n\n", // Paragraphs
			"\n",   // Lines
			". ",   // Sentences
			"! ",   // Exclamations
			"? ",   // Questions
			"; ",   // Semicolons
			", ",   // Commas
			" ",    // Words
			"",     // Characters
		},
	}
}

// Split splits a document recursively
func (s *RecursiveTextSplitter) Split(ctx context.Context, doc *Document) ([]*Document, error) {
	if doc == nil || doc.Content == "" {
		return []*Document{}, nil
	}

	chunks := s.splitTextRecursive(doc.Content, s.Separators)
	documents := make([]*Document, 0, len(chunks))

	for i, chunk := range chunks {
		newDoc := doc.Clone()
		newDoc.Content = chunk
		newDoc.ID = fmt.Sprintf("%s_chunk_%d", doc.ID, i)
		newDoc.Metadata[MetadataChunkIndex] = i
		newDoc.Metadata[MetadataChunkTotal] = len(chunks)
		newDoc.Metadata[MetadataDocumentID] = doc.ID

		documents = append(documents, newDoc)
	}

	return documents, nil
}

// SplitStream implements streaming split
func (s *RecursiveTextSplitter) SplitStream(ctx context.Context, stream DocumentStream) (DocumentStream, error) {
	return &recursiveSplitStream{
		source:   stream,
		splitter: s,
		ctx:      ctx,
	}, nil
}

// recursiveSplitStream implements DocumentStream for recursive splitting
type recursiveSplitStream struct {
	source      DocumentStream
	splitter    *RecursiveTextSplitter
	ctx         context.Context
	buffer      []*Document
	bufferIndex int
}

func (rss *recursiveSplitStream) Next() (*Document, error) {
	// Return buffered documents first
	if rss.bufferIndex < len(rss.buffer) {
		doc := rss.buffer[rss.bufferIndex]
		rss.bufferIndex++
		return doc, nil
	}

	// Reset buffer
	rss.buffer = nil
	rss.bufferIndex = 0

	// Get next document from source stream
	doc, err := rss.source.Next()
	if err != nil {
		return nil, err
	}

	// Split the document
	chunks, err := rss.splitter.Split(rss.ctx, doc)
	if err != nil {
		return nil, err
	}

	if len(chunks) == 0 {
		// No chunks produced, try next document
		return rss.Next()
	}

	// Buffer the chunks
	rss.buffer = chunks
	rss.bufferIndex = 0

	// Return first chunk
	result := rss.buffer[0]
	rss.bufferIndex = 1
	return result, nil
}

func (rss *recursiveSplitStream) Close() error {
	return rss.source.Close()
}

// splitTextRecursive recursively splits text
func (s *RecursiveTextSplitter) splitTextRecursive(text string, separators []string) []string {
	if len(text) <= s.ChunkSize {
		return []string{text}
	}

	if len(separators) == 0 {
		// No more separators, split by character
		return s.splitByCharacter(text)
	}

	separator := separators[0]
	remainingSeparators := separators[1:]

	var chunks []string
	if separator == "" {
		// Character-level split
		return s.splitByCharacter(text)
	}

	// Split by current separator
	parts := strings.Split(text, separator)
	currentChunk := ""

	for _, part := range parts {
		testChunk := currentChunk
		if testChunk != "" {
			testChunk += separator
		}
		testChunk += part

		if len(testChunk) > s.ChunkSize {
			// Current chunk is too big
			if currentChunk != "" {
				chunks = append(chunks, strings.TrimSpace(currentChunk))
			}

			// If single part is too big, split it recursively
			if len(part) > s.ChunkSize {
				subChunks := s.splitTextRecursive(part, remainingSeparators)
				chunks = append(chunks, subChunks...)
				currentChunk = ""
			} else {
				// Add overlap
				if s.ChunkOverlap > 0 && len(currentChunk) > s.ChunkOverlap {
					overlap := currentChunk[len(currentChunk)-s.ChunkOverlap:]
					currentChunk = overlap + separator + part
				} else {
					currentChunk = part
				}
			}
		} else {
			currentChunk = testChunk
		}
	}

	if currentChunk != "" {
		chunks = append(chunks, strings.TrimSpace(currentChunk))
	}

	return chunks
}

// splitByCharacter splits by character count
func (s *RecursiveTextSplitter) splitByCharacter(text string) []string {
	result := make([]string, 0)
	runes := []rune(text)

	for i := 0; i < len(runes); i += s.ChunkSize - s.ChunkOverlap {
		end := i + s.ChunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunk := string(runes[i:end])
		if strings.TrimSpace(chunk) != "" {
			result = append(result, chunk)
		}
	}

	return result
}

// ============================================================================
// Token Splitter - splits by token count (for LLMs)
// ============================================================================

// TokenSplitter splits text by token count
type TokenSplitter struct {
	ChunkSize    int
	ChunkOverlap int
	TokenCounter TokenCounter
	ModelName    string
}

// TokenCounter counts tokens in text
type TokenCounter func(text string) int

// NewTokenSplitter creates a new token splitter
func NewTokenSplitter(chunkSize, chunkOverlap int, counter TokenCounter) *TokenSplitter {
	return &TokenSplitter{
		ChunkSize:    chunkSize,
		ChunkOverlap: chunkOverlap,
		TokenCounter: counter,
	}
}

// Split splits by token count
func (s *TokenSplitter) Split(ctx context.Context, doc *Document) ([]*Document, error) {
	if doc == nil || doc.Content == "" {
		return []*Document{}, nil
	}

	// Simple implementation - split by words as proxy for tokens
	// In production, use a proper tokenizer like tiktoken
	words := strings.Fields(doc.Content)
	chunks := make([]string, 0)
	currentChunk := make([]string, 0)
	currentTokens := 0

	for _, word := range words {
		wordTokens := s.TokenCounter(word)

		if currentTokens+wordTokens > s.ChunkSize && len(currentChunk) > 0 {
			chunks = append(chunks, strings.Join(currentChunk, " "))

			// Keep overlap
			overlapStart := len(currentChunk) - s.ChunkOverlap
			if overlapStart < 0 {
				overlapStart = 0
			}
			currentChunk = currentChunk[overlapStart:]
			currentTokens = 0
			for _, w := range currentChunk {
				currentTokens += s.TokenCounter(w)
			}
		}

		currentChunk = append(currentChunk, word)
		currentTokens += wordTokens
	}

	if len(currentChunk) > 0 {
		chunks = append(chunks, strings.Join(currentChunk, " "))
	}

	// Convert to documents
	documents := make([]*Document, 0, len(chunks))
	for i, chunk := range chunks {
		newDoc := doc.Clone()
		newDoc.Content = chunk
		newDoc.ID = fmt.Sprintf("%s_chunk_%d", doc.ID, i)
		newDoc.Metadata[MetadataChunkIndex] = i
		newDoc.Metadata[MetadataChunkTotal] = len(chunks)
		newDoc.Metadata[MetadataDocumentID] = doc.ID

		documents = append(documents, newDoc)
	}

	return documents, nil
}

// SplitStream implements streaming split
func (s *TokenSplitter) SplitStream(ctx context.Context, stream DocumentStream) (DocumentStream, error) {
	return &tokenSplitStream{
		source:   stream,
		splitter: s,
		ctx:      ctx,
	}, nil
}

// tokenSplitStream implements DocumentStream for token splitting
type tokenSplitStream struct {
	source      DocumentStream
	splitter    *TokenSplitter
	ctx         context.Context
	buffer      []*Document
	bufferIndex int
}

func (tss *tokenSplitStream) Next() (*Document, error) {
	// Return buffered documents first
	if tss.bufferIndex < len(tss.buffer) {
		doc := tss.buffer[tss.bufferIndex]
		tss.bufferIndex++
		return doc, nil
	}

	// Reset buffer
	tss.buffer = nil
	tss.bufferIndex = 0

	// Get next document from source stream
	doc, err := tss.source.Next()
	if err != nil {
		return nil, err
	}

	// Split the document
	chunks, err := tss.splitter.Split(tss.ctx, doc)
	if err != nil {
		return nil, err
	}

	if len(chunks) == 0 {
		// No chunks produced, try next document
		return tss.Next()
	}

	// Buffer the chunks
	tss.buffer = chunks
	tss.bufferIndex = 0

	// Return first chunk
	result := tss.buffer[0]
	tss.bufferIndex = 1
	return result, nil
}

func (tss *tokenSplitStream) Close() error {
	return tss.source.Close()
}

// SimpleTokenCounter is a simple token counter (counts words)
func SimpleTokenCounter(text string) int {
	return len(strings.Fields(text))
}
