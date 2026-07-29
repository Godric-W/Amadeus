package openai

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/Godric-W/Amadeus/internal/config"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

func NewClient(provider config.ProviderConfig) (openaisdk.Client, error) {
	return newClient(provider, nil)
}

func newClient(provider config.ProviderConfig, httpClient option.HTTPClient) (openaisdk.Client, error) {
	if err := validateClientConfig(provider); err != nil {
		return openaisdk.Client{}, err
	}

	options := []option.RequestOption{
		option.WithAPIKey(provider.APIKey),
		option.WithBaseURL(strings.TrimSpace(provider.BaseURL)),
		option.WithRequestTimeout(provider.Timeout),
		option.WithMaxRetries(provider.MaxRetries),
	}
	if httpClient != nil {
		options = append(options, option.WithHTTPClient(httpClient))
	}

	return openaisdk.NewClient(options...), nil
}

func validateClientConfig(provider config.ProviderConfig) error {
	if strings.TrimSpace(provider.APIKey) == "" {
		return errors.New("provider API key is empty")
	}

	baseURL := strings.TrimSpace(provider.BaseURL)
	if baseURL == "" {
		return errors.New("provider base URL is empty")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return errors.New("provider base URL must be an absolute URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("provider base URL scheme must be http or https")
	}
	if provider.Timeout <= 0 {
		return errors.New("provider timeout must be greater than zero")
	}
	if provider.MaxRetries < 0 {
		return fmt.Errorf("provider max retries must not be negative: %d", provider.MaxRetries)
	}

	return nil
}
