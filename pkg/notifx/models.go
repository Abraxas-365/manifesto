package notifx

// EmailMessage represents an email to be sent.
type EmailMessage struct {
	From        string       `json:"from"`
	To          []string     `json:"to"`
	CC          []string     `json:"cc,omitempty"`
	BCC         []string     `json:"bcc,omitempty"`
	ReplyTo     string       `json:"reply_to,omitempty"`
	Subject     string       `json:"subject"`
	TextBody    string       `json:"text_body,omitempty"`
	HTMLBody    string       `json:"html_body,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

// Attachment represents an email attachment.
type Attachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Data        []byte `json:"-"`
}

// SendResult represents the outcome of a single email send attempt.
type SendResult struct {
	MessageID string `json:"message_id,omitempty"`
	To        string `json:"to"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
}
