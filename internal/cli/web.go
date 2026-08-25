package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/webfetch"
	"github.com/Godric-W/Amadeus/internal/websearch"
	"github.com/spf13/cobra"
)

func newWebCommand(flags *configFlags, options RootOptions) *cobra.Command {
	command := &cobra.Command{Use: "web", Short: "Diagnose configured web capabilities"}
	command.AddCommand(newWebCheckCommand(flags, options))
	return command
}

func newWebCheckCommand(flags *configFlags, options RootOptions) *cobra.Command {
	query := "amadeus connectivity check"
	urlValue := ""
	command := &cobra.Command{
		Use:   "check",
		Short: "Check web configuration, authentication, network, and response format",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			configured, _, err := loadEffectiveConfig(command, flags, options.Environment)
			if err != nil {
				return err
			}
			if !configured.Web.Fetch.Enabled && !configured.Web.Search.Enabled {
				return errors.New("web fetch and search are not enabled")
			}
			if configured.Web.Fetch.Enabled {
				fmt.Fprintln(command.OutOrStdout(), "web fetch: configured")
				if strings.TrimSpace(urlValue) != "" {
					fetcher := options.Bootstrap.WebFetcher
					if fetcher == nil {
						fetcher, err = webfetch.New(webfetch.Options{MaxBytes: configured.Web.Fetch.MaxBytes, MaxRedirects: configured.Web.Fetch.MaxRedirects, Timeout: configured.Web.Fetch.Timeout})
						if err != nil {
							return err
						}
					}
					document, fetchErr := fetcher.Fetch(command.Context(), urlValue)
					if fetchErr != nil {
						return fmt.Errorf("web fetch check failed: %w", fetchErr)
					}
					fmt.Fprintf(command.OutOrStdout(), "web fetch check: ok (%s, %d chars)\n", document.URL, len(document.Markdown))
				}
			}
			if configured.Web.Search.Enabled {
				search := options.Bootstrap.WebSearch
				if search == nil {
					provider, providerErr := websearch.NewProvider(websearch.ProviderOptions{Name: string(configured.Web.Search.Provider), APIKey: configured.Web.Search.APIKey, BaseURL: configured.Web.Search.BaseURL})
					if providerErr != nil {
						return providerErr
					}
					search, providerErr = websearch.NewService(provider, websearch.ServiceOptions{Timeout: configured.Web.Search.Timeout, MaxResults: configured.Web.Search.MaxResults})
					if providerErr != nil {
						return providerErr
					}
				}
				results, searchErr := search.Search(command.Context(), query, 1)
				if searchErr != nil {
					return fmt.Errorf("web search check failed: %w", searchErr)
				}
				fmt.Fprintf(command.OutOrStdout(), "web search check: ok (provider=%s, results=%d)\n", configured.Web.Search.Provider, len(results))
			}
			return nil
		},
	}
	command.Flags().StringVar(&query, "query", query, "search query used for the connectivity check")
	command.Flags().StringVar(&urlValue, "url", "", "optional public URL used to exercise web fetch")
	return command
}
