package ocr

// Options for OCR operations
type Options struct {
	// Model selection
	Model string

	// Language
	LanguageHints []string

	// Features to enable
	EnableLayout     bool
	EnableEntities   bool
	EnableTables     bool
	EnableFormFields bool
	EnableImages     bool
	EnableMarkdown   bool

	// Table options
	TableFormat TableFormat

	// Image options
	IncludeImageBase64 bool
	ImageFormat        string

	// Layout options
	IncludeBlocks     bool
	IncludeParagraphs bool
	IncludeLines      bool
	IncludeWords      bool

	// Document structure
	ExtractHeader bool
	ExtractFooter bool

	// Quality filters
	MinConfidence float32

	// Document hints
	DocumentType string // "invoice", "receipt", "form", "academic_paper"

	// Provider-specific
	ProviderOptions map[string]any
}

type Option func(*Options)

// Feature enablers
func WithLayout() Option {
	return func(o *Options) { o.EnableLayout = true }
}

func WithEntities() Option {
	return func(o *Options) { o.EnableEntities = true }
}

func WithTables(format TableFormat) Option {
	return func(o *Options) {
		o.EnableTables = true
		o.TableFormat = format
	}
}

func WithFormFields() Option {
	return func(o *Options) { o.EnableFormFields = true }
}

func WithImages(includeBase64 bool) Option {
	return func(o *Options) {
		o.EnableImages = true
		o.IncludeImageBase64 = includeBase64
	}
}

func WithMarkdown() Option {
	return func(o *Options) { o.EnableMarkdown = true }
}

// Granularity options
func WithWords() Option {
	return func(o *Options) { o.IncludeWords = true }
}

func WithLines() Option {
	return func(o *Options) { o.IncludeLines = true }
}

func WithParagraphs() Option {
	return func(o *Options) { o.IncludeParagraphs = true }
}

// Model and language
func WithModel(model string) Option {
	return func(o *Options) { o.Model = model }
}

func WithLanguageHints(langs ...string) Option {
	return func(o *Options) { o.LanguageHints = langs }
}

// Document type hints
func WithDocumentType(docType string) Option {
	return func(o *Options) { o.DocumentType = docType }
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

type TableFormat string

const (
	TableFormatHTML     TableFormat = "html"
	TableFormatMarkdown TableFormat = "markdown"
	TableFormatCSV      TableFormat = "csv"
)

func DefaultOptions() *Options {
	return &Options{
		EnableLayout:    true,
		ProviderOptions: make(map[string]any),
	}
}

func ApplyOptions(opts ...Option) *Options {
	options := DefaultOptions()
	for _, opt := range opts {
		opt(options)
	}
	return options
}
