package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/Godric-W/Amadeus/internal/config"
)

func TestNewHonorsConfiguredLevel(t *testing.T) {
	var output bytes.Buffer
	runtime, err := New(config.LoggingConfig{Level: config.LogLevelInfo}, &output)
	if err != nil {
		t.Fatalf("initialize logging: %v", err)
	}

	runtime.Logger().Debug("hidden debug message")
	runtime.Logger().Info("visible info message")

	logged := output.String()
	if strings.Contains(logged, "hidden debug message") {
		t.Fatalf("debug message passed info filter: %s", logged)
	}
	if !strings.Contains(logged, "visible info message") {
		t.Fatalf("info message was not logged: %s", logged)
	}
}

func TestNewRedactsSensitiveAttributes(t *testing.T) {
	var output bytes.Buffer
	runtime, err := New(config.LoggingConfig{Level: config.LogLevelInfo}, &output)
	if err != nil {
		t.Fatalf("initialize logging: %v", err)
	}

	runtime.Logger().Info(
		"provider request",
		slog.String("api_key", "api-secret"),
		slog.Group("headers", slog.String("Authorization", "Bearer auth-secret")),
		slog.String("model", "test-model"),
	)

	logged := output.String()
	for _, secret := range []string{"api-secret", "auth-secret"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("log contains secret %q: %s", secret, logged)
		}
	}
	if strings.Count(logged, config.RedactedSecret) != 2 {
		t.Fatalf("unexpected redaction output: %s", logged)
	}
	if !strings.Contains(logged, "test-model") {
		t.Fatalf("non-sensitive attribute was removed: %s", logged)
	}
}

func TestNewPreservesTraceLLMSetting(t *testing.T) {
	runtime, err := New(config.LoggingConfig{
		Level:    config.LogLevelWarn,
		TraceLLM: true,
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("initialize logging: %v", err)
	}

	if !runtime.TraceLLMEnabled() {
		t.Fatal("trace LLM setting was not preserved")
	}
}

func TestNewRejectsUnsupportedLevel(t *testing.T) {
	_, err := New(config.LoggingConfig{Level: config.LogLevel("verbose")}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected unsupported logging level error")
	}
	if !strings.Contains(err.Error(), "verbose") {
		t.Fatalf("error does not contain invalid level: %v", err)
	}
}

func TestNewRejectsNilOutput(t *testing.T) {
	_, err := New(config.LoggingConfig{Level: config.LogLevelInfo}, nil)
	if err == nil {
		t.Fatal("expected nil output error")
	}
}
