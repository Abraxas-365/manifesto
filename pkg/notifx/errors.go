package notifx

import "github.com/Abraxas-365/manifesto/pkg/errx"

var notifxErrors = errx.NewRegistry("NOTIFX")

var (
	ErrSendFailed      = notifxErrors.Register("SEND_FAILED", errx.TypeExternal, 500, "Failed to send email")
	ErrInvalidMessage  = notifxErrors.Register("INVALID_MESSAGE", errx.TypeValidation, 400, "Invalid email message")
	ErrTemplateNotFound = notifxErrors.Register("TEMPLATE_NOT_FOUND", errx.TypeNotFound, 404, "Email template not found")
	ErrTemplateParse   = notifxErrors.Register("TEMPLATE_PARSE", errx.TypeValidation, 400, "Failed to parse email template")
	ErrTemplateRender  = notifxErrors.Register("TEMPLATE_RENDER", errx.TypeInternal, 500, "Failed to render email template")
	ErrNoProvider      = notifxErrors.Register("NO_PROVIDER", errx.TypeInternal, 500, "No email provider configured")
)
