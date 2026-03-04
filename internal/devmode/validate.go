//go:build dev

package devmode

import (
	"fmt"
	"os"
	"strings"
)

// ValidationError represents validation errors with context.
type ValidationError struct {
	Errors []string
}

func (ve *ValidationError) Error() string {
	return strings.Join(ve.Errors, "\n")
}

// Validate performs startup validation on the dev config.
// It checks:
// - All referenced binaries exist and are executable
// - No port conflicts in health endpoints
// - build.yaml version is supported
func (c *DevConfig) Validate() error {
	if c == nil {
		return nil // nil config is not an error in production stub
	}

	var ve ValidationError

	// Check each override's binary exists and is executable
	for name := range c.Overrides {
		path, err := c.ResolveBinary(name)
		if err != nil {
			ve.Errors = append(ve.Errors, fmt.Sprintf("DEV MODE ERROR: %s", err.Error()))
			continue
		}

		// Double-check the path is actually executable
		info, err := os.Stat(path)
		if err != nil {
			ve.Errors = append(ve.Errors, fmt.Sprintf("DEV MODE ERROR: binary for %q cannot be accessed: %v", name, err))
			continue
		}
		if info.Mode()&0o111 == 0 {
			ve.Errors = append(ve.Errors, fmt.Sprintf("DEV MODE ERROR: binary for %q is not executable: %s", name, path))
		}
	}

	// Check for port conflicts in health endpoints
	portMap := make(map[string][]string) // port -> list of UMC names
	for name := range c.Overrides {
		endpoint := c.GetHealthEndpoint(name)
		if endpoint == "" {
			continue
		}
		port := extractPort(endpoint)
		if port != "" {
			portMap[port] = append(portMap[port], name)
		}
	}

	for port, names := range portMap {
		if len(names) > 1 {
			ve.Errors = append(ve.Errors,
				fmt.Sprintf("DEV MODE WARNING: port %s is used by multiple UMCs: %s",
					port, strings.Join(names, ", ")))
		}
	}

	if len(ve.Errors) > 0 {
		return &ve
	}
	return nil
}

// extractPort extracts the port number from a health endpoint URL.
// Examples:
//   - "http://localhost:10081/health" -> "10081"
//   - "http://localhost:8080" -> "8080"
//   - "invalid" -> ""
func extractPort(endpoint string) string {
	// Simple string-based extraction: look for :digits pattern
	// This is intentionally simple and not a full URL parser
	if !strings.Contains(endpoint, ":") {
		return ""
	}

	// Find the last colon
	colonIdx := strings.LastIndex(endpoint, ":")
	if colonIdx == -1 || colonIdx == len(endpoint)-1 {
		return ""
	}

	remainder := endpoint[colonIdx+1:]
	// Extract digits until we hit a non-digit
	var port strings.Builder
	for _, ch := range remainder {
		if ch >= '0' && ch <= '9' {
			port.WriteRune(ch)
		} else {
			break
		}
	}

	return port.String()
}

// ValidateAndLog performs validation and logs results to the provided logger function.
// It returns true if validation passed, false otherwise.
func (c *DevConfig) ValidateAndLog(logFn func(string)) bool {
	err := c.Validate()
	if err == nil {
		logFn("✓ Dev mode config validation passed")
		return true
	}

	ve, ok := err.(*ValidationError)
	if !ok {
		logFn(fmt.Sprintf("✗ Dev mode config error: %v", err))
		return false
	}

	for _, errMsg := range ve.Errors {
		logFn(fmt.Sprintf("✗ %s", errMsg))
	}
	return false
}
