package secrets

import (
	"fmt"
	"os"
	"strings"
)

// GetSecret retrieves a secret value, supporting both direct env vars and file-based secrets
// File-based format: /run/secrets/secret_name or any file path
// Env var format: SECRET_NAME
func GetSecret(envKey string, defaultValue string) (string, error) {
	// First, check if there's a _FILE variant (Docker secrets pattern)
	filePathKey := envKey + "_FILE"
	if filePath := os.Getenv(filePathKey); filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("read secret file %s: %w", filePath, err)
		}
		return strings.TrimSpace(string(data)), nil
	}

	// Fall back to direct environment variable
	if value := os.Getenv(envKey); value != "" {
		return value, nil
	}

	// Use default if provided
	if defaultValue != "" {
		return defaultValue, nil
	}

	return "", nil
}

// MustGetSecret retrieves a secret and panics if not found
func MustGetSecret(envKey string) string {
	value, err := GetSecret(envKey, "")
	if err != nil {
		panic(fmt.Sprintf("failed to load secret %s: %v", envKey, err))
	}
	if value == "" {
		panic(fmt.Sprintf("secret %s is required but not set", envKey))
	}
	return value
}

// GetOptionalSecret retrieves a secret with a default value, never fails
func GetOptionalSecret(envKey string, defaultValue string) string {
	value, err := GetSecret(envKey, defaultValue)
	if err != nil {
		return defaultValue
	}
	return value
}
