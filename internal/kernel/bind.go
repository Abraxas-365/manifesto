package kernel

import (
	"github.com/Abraxas-365/manifesto/internal/errx"
	"github.com/gofiber/fiber/v2"
)

// BindAndValidate parses the request body into T and calls its Validate method.
// If T does not implement Validate() error via a pointer receiver, the code will not compile.
func BindAndValidate[T any, PT interface {
	*T
	Validate() error
}](c *fiber.Ctx) (T, error) {
	var req T
	if err := c.BodyParser(&req); err != nil {
		return req, errx.Validation("Invalid request body")
	}
	if err := PT(&req).Validate(); err != nil {
		return req, err
	}
	return req, nil
}
