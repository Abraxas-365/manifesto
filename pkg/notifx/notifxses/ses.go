package notifxses

import (
	"context"

	"github.com/Abraxas-365/manifesto/pkg/notifx"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/ses/types"
)

// SESProvider implements notifx.EmailSender and notifx.BulkEmailSender using AWS SES.
type SESProvider struct {
	client      *ses.Client
	fromAddress string
}

// NewSESProvider creates a new SES email provider.
func NewSESProvider(client *ses.Client, fromAddress string) *SESProvider {
	return &SESProvider{
		client:      client,
		fromAddress: fromAddress,
	}
}

// SendEmail sends a single email via SES.
func (p *SESProvider) SendEmail(ctx context.Context, msg notifx.EmailMessage, opts ...notifx.Option) error {
	from := msg.From
	if from == "" {
		from = p.fromAddress
	}

	dest := &types.Destination{
		ToAddresses:  msg.To,
		CcAddresses:  msg.CC,
		BccAddresses: msg.BCC,
	}

	body := &types.Body{}
	if msg.TextBody != "" {
		body.Text = &types.Content{
			Data:    aws.String(msg.TextBody),
			Charset: aws.String("UTF-8"),
		}
	}
	if msg.HTMLBody != "" {
		body.Html = &types.Content{
			Data:    aws.String(msg.HTMLBody),
			Charset: aws.String("UTF-8"),
		}
	}

	input := &ses.SendEmailInput{
		Source:      aws.String(from),
		Destination: dest,
		Message: &types.Message{
			Subject: &types.Content{
				Data:    aws.String(msg.Subject),
				Charset: aws.String("UTF-8"),
			},
			Body: body,
		},
	}

	if msg.ReplyTo != "" {
		input.ReplyToAddresses = []string{msg.ReplyTo}
	}

	_, err := p.client.SendEmail(ctx, input)
	if err != nil {
		return sesErrors.NewWithCause(ErrSendFailed, err).
			WithDetail("to", msg.To).
			WithDetail("subject", msg.Subject)
	}

	return nil
}

// SendBulkEmail sends multiple emails individually via SES.
func (p *SESProvider) SendBulkEmail(ctx context.Context, msgs []notifx.EmailMessage, opts ...notifx.Option) ([]notifx.SendResult, error) {
	results := make([]notifx.SendResult, len(msgs))

	for i, msg := range msgs {
		to := ""
		if len(msg.To) > 0 {
			to = msg.To[0]
		}

		err := p.SendEmail(ctx, msg, opts...)
		results[i] = notifx.SendResult{
			To:      to,
			Success: err == nil,
		}
		if err != nil {
			results[i].Error = err.Error()
		}
	}

	return results, nil
}
