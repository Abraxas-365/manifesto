package skill

import "strings"

// Registry holds the set of skills available to the Skill tool.
type Registry struct {
	skills map[string]*Skill
	order  []string
}

// NewRegistry returns an empty skill registry.
func NewRegistry() *Registry {
	return &Registry{skills: make(map[string]*Skill)}
}

// Register adds a skill, keyed by its name. Re-registering a name replaces it
// while preserving order.
func (r *Registry) Register(s *Skill) {
	name := s.Name
	if _, exists := r.skills[name]; !exists {
		r.order = append(r.order, name)
	}
	r.skills[name] = s
}

// Get returns a skill by name, falling back to a case-insensitive match.
func (r *Registry) Get(name string) (*Skill, bool) {
	if s, ok := r.skills[name]; ok {
		return s, true
	}
	for _, n := range r.order {
		if strings.EqualFold(n, name) {
			return r.skills[n], true
		}
	}
	return nil, false
}

// All returns the registered skills in registration order.
func (r *Registry) All() []*Skill {
	out := make([]*Skill, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.skills[name])
	}
	return out
}

// Names returns the registered skill names in order.
func (r *Registry) Names() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}
