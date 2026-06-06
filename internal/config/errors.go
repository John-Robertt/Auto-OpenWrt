package config

import "fmt"

type ConfigError struct {
	Code       string
	Path       string
	Message    string
	Suggestion string
	Details    map[string]any
}

func (e *ConfigError) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("%s: %s: %s", e.Code, e.Path, e.Message)
}

func newConfigError(code, path, message, suggestion string) *ConfigError {
	return &ConfigError{
		Code:       code,
		Path:       path,
		Message:    message,
		Suggestion: suggestion,
		Details:    map[string]any{"field": path},
	}
}
