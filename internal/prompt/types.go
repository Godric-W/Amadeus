package prompt

import (
	"errors"
	"fmt"
	"strings"
)

type NamedBundle struct {
	ID     string `json:"id"`
	Bundle Bundle `json:"bundle"`
}

func (bundle NamedBundle) Validate() error {
	if strings.TrimSpace(bundle.ID) == "" {
		return errors.New("named Prompt bundle ID is empty")
	}
	if err := ValidateBundle(bundle.Bundle); err != nil {
		return fmt.Errorf("named Prompt bundle %q: %w", bundle.ID, err)
	}
	return nil
}

func ValidateBundle(bundle Bundle) error {
	if strings.TrimSpace(bundle.Content) == "" {
		return errors.New("Prompt bundle content is empty")
	}
	if len(bundle.Sources) == 0 {
		return errors.New("Prompt bundle sources are empty")
	}
	if !validSHA256(bundle.SHA256) {
		return errors.New("Prompt bundle SHA-256 is invalid")
	}
	if contentHash(bundle.Content) != bundle.SHA256 {
		return errors.New("Prompt bundle SHA-256 does not match content")
	}
	for index, source := range bundle.Sources {
		if strings.TrimSpace(source.Kind) == "" || strings.TrimSpace(source.Path) == "" || !validSHA256(source.SHA256) {
			return fmt.Errorf("Prompt bundle source %d is invalid", index)
		}
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
