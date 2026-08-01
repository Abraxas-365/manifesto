package agentdef

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// frontmatter is the YAML header of an AGENT.md / <name>.md file. Accepts
// both legacy (autoLoadSkills, comma-separated tools) and pi-style (list) forms.
type frontmatter struct {
	Name             string    `yaml:"name"`
	Description      string    `yaml:"description"`
	Model            string    `yaml:"model"`
	Thinking         string    `yaml:"thinking"`
	SystemPromptMode string    `yaml:"systemPromptMode"`
	Tools            stringSet `yaml:"tools"`
	ToolExclude      stringSet `yaml:"toolExclude"`
	AutoLoadSkills   stringSet `yaml:"autoLoadSkills"`
	Subagents        stringSet `yaml:"subagents"`
	MaxSubagentDepth int       `yaml:"maxSubagentDepth"`
	MaxTurns         int       `yaml:"maxTurns"`
	GraceTurns       int       `yaml:"graceTurns"`
	ReadOnly         bool      `yaml:"readOnly"`
}

// stringSet unmarshals either a YAML list or a comma-separated string.
type stringSet []string

func (s *stringSet) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		var raw string
		if err := node.Decode(&raw); err != nil {
			return err
		}
		for _, part := range strings.Split(raw, ",") {
			if p := strings.TrimSpace(part); p != "" {
				*s = append(*s, p)
			}
		}
		return nil
	default:
		var list []string
		if err := node.Decode(&list); err != nil {
			return err
		}
		*s = list
		return nil
	}
}

// ParseMarkdown parses an agent definition from markdown bytes: YAML
// frontmatter + body as system prompt. defaultName is used when the
// frontmatter has no name (legacy: file/dir basename).
func ParseMarkdown(md []byte, defaultName string) (Definition, error) {
	fm, body, err := splitFrontmatter(string(md))
	if err != nil {
		return Definition{}, err
	}
	name := fm.Name
	if name == "" {
		name = defaultName
	}
	if strings.TrimSpace(name) == "" {
		return Definition{}, fmt.Errorf("agentdef: agent has no name")
	}
	return Definition{
		Name:             name,
		Description:      fm.Description,
		Model:            fm.Model,
		Thinking:         fm.Thinking,
		SystemPrompt:     strings.TrimSpace(body),
		SystemPromptMode: fm.SystemPromptMode,
		Tools:            fm.Tools,
		ToolExclude:      fm.ToolExclude,
		AutoloadSkills:   fm.AutoLoadSkills,
		Subagents:        fm.Subagents,
		MaxSubagentDepth: fm.MaxSubagentDepth,
		MaxTurns:         fm.MaxTurns,
		GraceTurns:       fm.GraceTurns,
		ReadOnly:         fm.ReadOnly,
	}, nil
}

func splitFrontmatter(md string) (frontmatter, string, error) {
	var fm frontmatter
	s := strings.TrimLeft(md, "\ufeff \t\r\n")
	if !strings.HasPrefix(s, "---") {
		return fm, md, nil
	}
	rest := strings.TrimPrefix(strings.TrimPrefix(s[3:], "\r"), "\n")
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return fm, md, nil
	}
	yamlBlock := rest[:idx]
	body := rest[idx+len("\n---"):]
	if nl := strings.IndexByte(body, '\n'); nl >= 0 {
		body = body[nl+1:]
	} else {
		body = ""
	}
	if err := yaml.Unmarshal([]byte(yamlBlock), &fm); err != nil {
		return fm, "", fmt.Errorf("agentdef: frontmatter: %w", err)
	}
	return fm, body, nil
}

// AgentsDirName is the project-relative directory scanned for agent
// definitions. Override at startup to brand it for your product.
var AgentsDirName = ".agent"

// DiscoverDirs returns the standard agent definition directories for a
// project, lowest precedence first: user <userHome>/agents, project
// <projectDir>/<AgentsDirName>/agents (later wins on name clashes).
func DiscoverDirs(userHome, projectDir string) []string {
	return []string{
		filepath.Join(userHome, "agents"),
		filepath.Join(projectDir, AgentsDirName, "agents"),
	}
}

// LoadDir loads agent definitions from dir into r. Two layouts, matching legacy
// load_dir plus pi-subagents flat files:
//
//	dir/<name>.md            (flat file; name defaults to basename)
//	dir/<name>/AGENT.md      (subdir; name defaults to dir basename)
//
// A missing dir is fine. Broken files are skipped and reported in errs.
func LoadDir(r *Registry, dir string) (loaded []string, errs []error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil // missing dir is fine
	}
	for _, e := range entries {
		var path, defaultName string
		if e.IsDir() {
			path = filepath.Join(dir, e.Name(), "AGENT.md")
			defaultName = e.Name()
		} else if strings.HasSuffix(e.Name(), ".md") {
			path = filepath.Join(dir, e.Name())
			defaultName = strings.TrimSuffix(e.Name(), ".md")
		} else {
			continue
		}
		md, err := os.ReadFile(path)
		if err != nil {
			continue // subdir without AGENT.md, unreadable file
		}
		def, err := ParseMarkdown(md, defaultName)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			continue
		}
		if err := r.Define(def); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			continue
		}
		loaded = append(loaded, def.Name)
	}
	return loaded, errs
}
