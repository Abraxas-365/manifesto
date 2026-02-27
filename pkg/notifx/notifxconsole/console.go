package notifxconsole

import (
	"context"
	"strings"

	"github.com/Abraxas-365/manifesto/pkg/logx"
	"github.com/Abraxas-365/manifesto/pkg/notifx"
)

// ConsoleProvider prints emails to the terminal via logx. Intended for development and testing.
type ConsoleProvider struct{}

// NewConsoleProvider creates a new console email provider.
func NewConsoleProvider() *ConsoleProvider {
	return &ConsoleProvider{}
}

// SendEmail logs the email details instead of sending it.
func (p *ConsoleProvider) SendEmail(_ context.Context, msg notifx.EmailMessage, _ ...notifx.Option) error {
	logx.WithFields(logx.Fields{
		"from":    msg.From,
		"to":      strings.Join(msg.To, ", "),
		"subject": msg.Subject,
	}).Info("notifx/console: email sent (dev mode)")

	if msg.TextBody != "" {
		logx.Debugf("notifx/console: text body:\n%s", msg.TextBody)
	}
	if msg.HTMLBody != "" {
		logx.Debugf("notifx/console: html body:\n%s", msg.HTMLBody)
	}

	return nil
}
