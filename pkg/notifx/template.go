package notifx

import (
	"bytes"
	"html/template"
	"sync"
)

// TemplateRegistry stores and renders named Go html/templates.
type TemplateRegistry struct {
	templates map[string]*template.Template
	mu        sync.RWMutex
}

// NewTemplateRegistry creates a new template registry.
func NewTemplateRegistry() *TemplateRegistry {
	return &TemplateRegistry{
		templates: make(map[string]*template.Template),
	}
}

// Register parses and stores a template by name.
func (r *TemplateRegistry) Register(name, tmplString string) error {
	t, err := template.New(name).Parse(tmplString)
	if err != nil {
		return notifxErrors.NewWithCause(ErrTemplateParse, err).WithDetail("template", name)
	}

	r.mu.Lock()
	r.templates[name] = t
	r.mu.Unlock()

	return nil
}

// Render executes a named template with the given data and returns the result.
func (r *TemplateRegistry) Render(name string, data interface{}) (string, error) {
	r.mu.RLock()
	t, ok := r.templates[name]
	r.mu.RUnlock()

	if !ok {
		return "", notifxErrors.New(ErrTemplateNotFound).WithDetail("template", name)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", notifxErrors.NewWithCause(ErrTemplateRender, err).WithDetail("template", name)
	}

	return buf.String(), nil
}
