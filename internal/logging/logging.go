package logging

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"unicode"

	"github.com/Godric-W/Amadeus/internal/config"
)

type Runtime struct {
	logger   *slog.Logger
	traceLLM bool
}

func New(settings config.LoggingConfig, output io.Writer) (Runtime, error) {
	if output == nil {
		return Runtime{}, errors.New("logging output is nil")
	}

	level, err := parseLevel(settings.Level)
	if err != nil {
		return Runtime{}, err
	}

	handler := slog.NewJSONHandler(output, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, attribute slog.Attr) slog.Attr {
			if isSensitiveKey(attribute.Key) {
				return slog.String(attribute.Key, config.RedactedSecret)
			}
			return attribute
		},
	})

	return Runtime{
		logger:   slog.New(handler),
		traceLLM: settings.TraceLLM,
	}, nil
}

func (runtime Runtime) Logger() *slog.Logger {
	return runtime.logger
}

func (runtime Runtime) TraceLLMEnabled() bool {
	return runtime.traceLLM
}

func parseLevel(level config.LogLevel) (slog.Level, error) {
	switch level {
	case config.LogLevelDebug:
		return slog.LevelDebug, nil
	case config.LogLevelInfo:
		return slog.LevelInfo, nil
	case config.LogLevelWarn:
		return slog.LevelWarn, nil
	case config.LogLevelError:
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unsupported logging level %q", level)
	}
}

func isSensitiveKey(key string) bool {
	normalized := strings.Map(func(character rune) rune {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			return unicode.ToLower(character)
		}
		return -1
	}, key)

	for _, suffix := range []string{
		"apikey",
		"authorization",
		"accesstoken",
		"refreshtoken",
		"clientsecret",
		"password",
		"credential",
		"credentials",
	} {
		if strings.HasSuffix(normalized, suffix) {
			return true
		}
	}

	switch normalized {
	case "token", "secret", "cookie", "setcookie":
		return true
	default:
		return false
	}
}
