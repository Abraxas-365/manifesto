package vstore

// Options for vector store operations
type Options struct {
	// Namespace/partition
	Namespace string

	// TopK results to return
	TopK int

	// IncludeValues in search results
	IncludeValues bool

	// IncludeMetadata in search results
	IncludeMetadata bool

	// MinScore threshold for results
	MinScore float32

	// Filter for metadata filtering
	Filter *Filter

	// HybridAlpha for hybrid search (0 = keyword, 1 = vector)
	HybridAlpha float32

	// SparseValues for hybrid dense/sparse search
	SparseValues *SparseVector

	// BatchSize for batch operations
	BatchSize int

	// Provider-specific options
	ProviderOptions map[string]any
}

type Option func(*Options)

// Namespace options
func WithNamespace(namespace string) Option {
	return func(o *Options) {
		o.Namespace = namespace
	}
}

// Query options
func WithTopK(k int) Option {
	return func(o *Options) {
		o.TopK = k
	}
}

func WithIncludeValues(include bool) Option {
	return func(o *Options) {
		o.IncludeValues = include
	}
}

func WithIncludeMetadata(include bool) Option {
	return func(o *Options) {
		o.IncludeMetadata = include
	}
}

func WithMinScore(score float32) Option {
	return func(o *Options) {
		o.MinScore = score
	}
}

// Filter options
func WithFilter(filter *Filter) Option {
	return func(o *Options) {
		o.Filter = filter
	}
}

// Hybrid search options
func WithHybridAlpha(alpha float32) Option {
	return func(o *Options) {
		o.HybridAlpha = alpha
	}
}

func WithSparseValues(sparse *SparseVector) Option {
	return func(o *Options) {
		o.SparseValues = sparse
	}
}

// Batch options
func WithBatchSize(size int) Option {
	return func(o *Options) {
		o.BatchSize = size
	}
}

// Provider-specific options
func WithProviderOption(key string, value any) Option {
	return func(o *Options) {
		if o.ProviderOptions == nil {
			o.ProviderOptions = make(map[string]any)
		}
		o.ProviderOptions[key] = value
	}
}

// DefaultOptions returns default options
func DefaultOptions() *Options {
	return &Options{
		Namespace:       "",
		TopK:            10,
		IncludeValues:   false,
		IncludeMetadata: true,
		MinScore:        0,
		HybridAlpha:     0.5,
		BatchSize:       100,
		ProviderOptions: make(map[string]any),
	}
}

// ApplyOptions applies options
func ApplyOptions(opts ...Option) *Options {
	options := DefaultOptions()
	for _, opt := range opts {
		opt(options)
	}
	return options
}
