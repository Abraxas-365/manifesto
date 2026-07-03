package exec

import (
	"net/http"

	"github.com/Abraxas-365/manifesto/internal/errx"
)

// Registry holds the error codes for the harness executor layer.
var Registry = errx.NewRegistry("HARNESS_EXEC")

var (
	ErrCommandFailed = Registry.Register(
		"COMMAND_FAILED",
		errx.TypeExternal,
		http.StatusInternalServerError,
		"Command execution failed",
	)

	ErrTimeout = Registry.Register(
		"TIMEOUT",
		errx.TypeExternal,
		http.StatusRequestTimeout,
		"Command timed out",
	)
)
