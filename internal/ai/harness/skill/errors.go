package skill

import (
	"net/http"

	"github.com/Abraxas-365/manifesto/internal/errx"
)

var errRegistry = errx.NewRegistry("HARNESS_SKILL")

var (
	// ErrUnknownSkill is returned when a skill name is not registered.
	ErrUnknownSkill = errRegistry.Register(
		"UNKNOWN_SKILL",
		errx.TypeNotFound,
		http.StatusNotFound,
		"Unknown skill",
	)

	// ErrSkillLoad is returned when a skill's source cannot be read or materialized.
	ErrSkillLoad = errRegistry.Register(
		"SKILL_LOAD",
		errx.TypeExternal,
		http.StatusBadGateway,
		"Failed to load skill",
	)

	// ErrParseFrontmatter is returned when a SKILL.md frontmatter cannot be parsed.
	ErrParseFrontmatter = errRegistry.Register(
		"PARSE_FRONTMATTER",
		errx.TypeValidation,
		http.StatusBadRequest,
		"Failed to parse skill frontmatter",
	)
)
