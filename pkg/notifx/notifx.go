package notifx

import (
	"context"
)

// EmailSender sends a single email.
type EmailSender interface {
	SendEmail(ctx context.Context, msg EmailMessage, opts ...Option) error
}

// BulkEmailSender sends multiple emails in a batch.
type BulkEmailSender interface {
	SendBulkEmail(ctx context.Context, msgs []EmailMessage, opts ...Option) ([]SendResult, error)
}

// Notifier is the high-level notification interface.
type Notifier interface {
	EmailSender
}

// Client is the main entry point for sending notifications.
type Client struct {
	provider  EmailSender
	templates *TemplateRegistry
}

// NewClient creates a new notification client.
func NewClient(provider EmailSender) *Client {
	return &Client{
		provider:  provider,
		templates: NewTemplateRegistry(),
	}
}

// SendEmail sends an email through the configured provider.
func (c *Client) SendEmail(ctx context.Context, msg EmailMessage, opts ...Option) error {
	if len(msg.To) == 0 {
		return notifxErrors.New(ErrInvalidMessage).WithDetail("reason", "no recipients")
	}
	if msg.Subject == "" {
		return notifxErrors.New(ErrInvalidMessage).WithDetail("reason", "empty subject")
	}
	return c.provider.SendEmail(ctx, msg, opts...)
}

// RegisterTemplate parses and stores a named template for later use.
func (c *Client) RegisterTemplate(name, tmplString string) error {
	return c.templates.Register(name, tmplString)
}

// SendTemplatedEmail renders a template and sends the resulting email.
func (c *Client) SendTemplatedEmail(ctx context.Context, templateName string, data interface{}, msg EmailMessage, opts ...Option) error {
	body, err := c.templates.Render(templateName, data)
	if err != nil {
		return err
	}

	msg.HTMLBody = body
	return c.SendEmail(ctx, msg, opts...)
}
