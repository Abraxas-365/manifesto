package config

// NotifxConfig configures the notification system.
type NotifxConfig struct {
	Provider    string
	FromAddress string
	FromName    string
	AWSRegion   string
}

func loadNotifxConfig() NotifxConfig {
	return NotifxConfig{
		Provider:    getEnv("NOTIFX_PROVIDER", "console"),
		FromAddress: getEnv("NOTIFX_FROM_ADDRESS", getEnv("EMAIL_FROM_ADDRESS", "noreply@manifesto.com")),
		FromName:    getEnv("NOTIFX_FROM_NAME", getEnv("EMAIL_FROM_NAME", "Manifesto")),
		AWSRegion:   getEnv("NOTIFX_AWS_REGION", getEnv("AWS_REGION", "us-east-1")),
	}
}
