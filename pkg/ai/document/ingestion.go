package document

import (
	"context"
	"fmt"
	"io"
	"sync"
)

// ============================================================================
// Ingestion Pipeline - processes and stores documents
// ============================================================================

// IngestionPipeline orchestrates document processing and storage
type IngestionPipeline struct {
	loader       Loader
	splitter     Splitter
	store        *DocumentStore
	transformers []DocumentTransformer
	filters      []DocumentFilter

	// Performance settings
	concurrency  int
	errorHandler ErrorHandler
}

// ErrorHandler handles errors during ingestion
type ErrorHandler func(doc *Document, err error) error

// NewIngestionPipeline creates a new ingestion pipeline
func NewIngestionPipeline(loader Loader, store *DocumentStore) *IngestionPipeline {
	return &IngestionPipeline{
		loader:       loader,
		store:        store,
		concurrency:  4,
		errorHandler: defaultErrorHandler,
	}
}

// WithSplitter sets the document splitter
func (p *IngestionPipeline) WithSplitter(splitter Splitter) *IngestionPipeline {
	p.splitter = splitter
	return p
}

// WithTransformer adds a document transformer
func (p *IngestionPipeline) WithTransformer(transformer DocumentTransformer) *IngestionPipeline {
	p.transformers = append(p.transformers, transformer)
	return p
}

// WithFilter adds a document filter
func (p *IngestionPipeline) WithFilter(filter DocumentFilter) *IngestionPipeline {
	p.filters = append(p.filters, filter)
	return p
}

// WithConcurrency sets the number of concurrent workers
func (p *IngestionPipeline) WithConcurrency(n int) *IngestionPipeline {
	p.concurrency = n
	return p
}

// WithErrorHandler sets a custom error handler
func (p *IngestionPipeline) WithErrorHandler(handler ErrorHandler) *IngestionPipeline {
	p.errorHandler = handler
	return p
}

// ============================================================================
// Run Pipeline
// ============================================================================

// Run executes the ingestion pipeline
func (p *IngestionPipeline) Run(ctx context.Context) (*IngestionResult, error) {
	// Load documents as stream
	stream, err := p.loader.LoadStream(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load documents: %w", err)
	}
	defer stream.Close()

	// Apply splitter if set
	if p.splitter != nil {
		stream, err = p.splitter.SplitStream(ctx, stream)
		if err != nil {
			return nil, fmt.Errorf("failed to split documents: %w", err)
		}
	}

	// Apply transformers
	for _, transformer := range p.transformers {
		stream, err = transformer.Transform(ctx, stream)
		if err != nil {
			return nil, fmt.Errorf("failed to transform documents: %w", err)
		}
	}

	// Apply filters
	if len(p.filters) > 0 {
		stream = &filteredStream{
			stream:  stream,
			filters: p.filters,
		}
	}

	// Ingest with concurrency
	return p.ingestConcurrently(ctx, stream)
}

// ingestConcurrently ingests documents using concurrent workers
func (p *IngestionPipeline) ingestConcurrently(ctx context.Context, stream DocumentStream) (*IngestionResult, error) {
	result := &IngestionResult{}
	var mu sync.Mutex

	// Channel for documents
	docChan := make(chan *Document, p.concurrency*2)
	errorChan := make(chan error, p.concurrency)

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < p.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			batch := make([]*Document, 0, p.store.batchSize)

			for doc := range docChan {
				batch = append(batch, doc)

				// Process batch when full
				if len(batch) >= p.store.batchSize {
					if err := p.store.AddDocuments(ctx, batch); err != nil {
						errorChan <- err
						mu.Lock()
						result.FailedCount += len(batch)
						mu.Unlock()
					} else {
						mu.Lock()
						result.ProcessedCount += len(batch)
						mu.Unlock()
					}
					batch = batch[:0]
				}
			}

			// Process remaining batch
			if len(batch) > 0 {
				if err := p.store.AddDocuments(ctx, batch); err != nil {
					errorChan <- err
					mu.Lock()
					result.FailedCount += len(batch)
					mu.Unlock()
				} else {
					mu.Lock()
					result.ProcessedCount += len(batch)
					mu.Unlock()
				}
			}
		}()
	}

	// Feed documents to workers
	go func() {
		defer close(docChan)

		for {
			doc, err := stream.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				errorChan <- err
				continue
			}

			select {
			case docChan <- doc:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Wait for workers
	wg.Wait()
	close(errorChan)

	// Collect errors
	for err := range errorChan {
		result.Errors = append(result.Errors, err)
	}

	return result, nil
}

// IngestionResult contains the results of an ingestion run
type IngestionResult struct {
	ProcessedCount int
	FailedCount    int
	Errors         []error
}

func defaultErrorHandler(doc *Document, err error) error {
	// Log and continue
	return nil
}

// filteredStream applies filters to a document stream
type filteredStream struct {
	stream  DocumentStream
	filters []DocumentFilter
}

func (s *filteredStream) Next() (*Document, error) {
	for {
		doc, err := s.stream.Next()
		if err != nil {
			return nil, err
		}

		// Apply filters
		keep := true
		for _, filter := range s.filters {
			if !filter(doc) {
				keep = false
				break
			}
		}

		if keep {
			return doc, nil
		}
		// Skip filtered document, get next
	}
}

func (s *filteredStream) Close() error {
	return s.stream.Close()
}
