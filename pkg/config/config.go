package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Server       ServerConfig
	Database     DatabaseConfig
	Redis        RedisConfig
	Auth         AuthConfig
	OAuth        OAuthConfig
	Email        EmailConfig
	SMS          SMSConfig
	TenantConfig TenantConfig
	Environment  Environment
}

type Environment string

const (
	EnvironmentDevelopment Environment = "development"
	EnvironmentStaging     Environment = "staging"
	EnvironmentProduction  Environment = "production"
)

func (c Config) IsDevelopment() bool {
	return c.Environment == EnvironmentDevelopment
}
func (c Config) IsStaging() bool {
	return c.Environment == EnvironmentStaging
}
func (c Config) IsProd() bool {
	return c.Environment == EnvironmentProduction
}

func loadEnvironment() Environment {
	env := getEnv("ENVIRONMENT", "development")
	switch strings.ToLower(env) {
	case "production":
		return EnvironmentProduction
	case "staging":
		return EnvironmentStaging
	default:
		return EnvironmentDevelopment
	}
}

func Load() (*Config, error) {
	cfg := &Config{
		Server:       loadServerConfig(),
		Database:     loadDatabaseConfig(),
		Redis:        loadRedisConfig(),
		Auth:         loadAuthConfig(),
		OAuth:        loadOAuthConfig(),
		Email:        loadEmailConfig(),
		SMS:          loadSMSConfig(),
		TenantConfig: loadTenantConfig(),
		Environment:  loadEnvironment(),
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	return nil
}

func (c *Config) IsProduction() bool {
	return c.Server.Environment == "production"
}

// Helper functions
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolVal, err := strconv.ParseBool(value); err == nil {
			return boolVal
		}
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

func getEnvStringSlice(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		return strings.Split(value, ",")
	}
	return defaultValue
}
