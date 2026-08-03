package mcp

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Godric-W/Amadeus/internal/config"
	"github.com/Godric-W/Amadeus/internal/project"
	"go.yaml.in/yaml/v3"
)

const (
	configFileName = "mcp.yaml"
	RedactedSecret = "[REDACTED]"
)

type Transport string

const (
	TransportStdio          Transport = "stdio"
	TransportStreamableHTTP Transport = "streamable_http"
)

type ServerConfig struct {
	Transport Transport         `yaml:"transport"`
	Command   string            `yaml:"command,omitempty"`
	Args      []string          `yaml:"args,omitempty"`
	Env       map[string]string `yaml:"env,omitempty"`
	URL       string            `yaml:"url,omitempty"`
	Headers   map[string]string `yaml:"headers,omitempty"`
	Timeout   time.Duration     `yaml:"timeout,omitempty"`
	Enabled   *bool             `yaml:"enabled,omitempty"`
}

type Config struct {
	Servers map[string]ServerConfig `yaml:"servers"`
}

type LoadOptions struct {
	LookupEnv config.EnvLookup
}

func Load(userRoot string, root project.Root, options LoadOptions) (Config, error) {
	if root.Path() == "" {
		return Config{}, errors.New("MCP project root is empty")
	}
	lookup := options.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	user := Config{Servers: map[string]ServerConfig{}}
	var err error
	if strings.TrimSpace(userRoot) != "" {
		user, err = loadFile(filepath.Join(userRoot, configFileName), lookup)
	}
	if err != nil {
		return Config{}, fmt.Errorf("load user MCP config: %w", err)
	}
	projectConfig, err := loadFile(filepath.Join(root.Path(), ".amadeus", configFileName), lookup)
	if err != nil {
		return Config{}, fmt.Errorf("load project MCP config: %w", err)
	}
	merged := Config{Servers: make(map[string]ServerConfig, len(user.Servers)+len(projectConfig.Servers))}
	for name, server := range user.Servers {
		merged.Servers[name] = cloneServer(server)
	}
	for name, server := range projectConfig.Servers {
		merged.Servers[name] = cloneServer(server)
	}
	if err := merged.Validate(); err != nil {
		return Config{}, err
	}
	return merged, nil
}

func (configured Config) Validate() error {
	for name, server := range configured.Servers {
		if !validServerName(name) {
			return fmt.Errorf("MCP server name %q is invalid", name)
		}
		if server.Transport != TransportStdio && server.Transport != TransportStreamableHTTP {
			return fmt.Errorf("MCP server %q transport %q is unsupported", name, server.Transport)
		}
		if server.Timeout < 0 {
			return fmt.Errorf("MCP server %q timeout cannot be negative", name)
		}
		switch server.Transport {
		case TransportStdio:
			if strings.TrimSpace(server.Command) == "" {
				return fmt.Errorf("MCP stdio server %q command is empty", name)
			}
			if strings.TrimSpace(server.URL) != "" || len(server.Headers) > 0 {
				return fmt.Errorf("MCP stdio server %q cannot set url or headers", name)
			}
		case TransportStreamableHTTP:
			if strings.TrimSpace(server.URL) == "" {
				return fmt.Errorf("MCP HTTP server %q url is empty", name)
			}
			if strings.TrimSpace(server.Command) != "" || len(server.Args) > 0 || len(server.Env) > 0 {
				return fmt.Errorf("MCP HTTP server %q cannot set command, args, or env", name)
			}
		}
	}
	return nil
}

func (configured Config) Server(name string) (ServerConfig, bool) {
	server, ok := configured.Servers[strings.TrimSpace(name)]
	return cloneServer(server), ok
}

func (configured Config) EnabledServers() []string {
	values := make([]string, 0, len(configured.Servers))
	for name, server := range configured.Servers {
		if server.IsEnabled() {
			values = append(values, name)
		}
	}
	sort.Strings(values)
	return values
}

func (server ServerConfig) IsEnabled() bool {
	return server.Enabled == nil || *server.Enabled
}

func (configured Config) Redacted() Config {
	result := Config{Servers: make(map[string]ServerConfig, len(configured.Servers))}
	for name, server := range configured.Servers {
		server = cloneServer(server)
		for key, value := range server.Env {
			if value != "" {
				server.Env[key] = RedactedSecret
			}
		}
		for key, value := range server.Headers {
			if value != "" {
				server.Headers[key] = RedactedSecret
			}
		}
		result.Servers[name] = server
	}
	return result
}

func loadFile(path string, lookup config.EnvLookup) (Config, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{Servers: map[string]ServerConfig{}}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("open %q: %w", path, err)
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		return Config{}, fmt.Errorf("read %q: %w", path, err)
	}
	if len(bytes.TrimSpace(content)) == 0 {
		return Config{Servers: map[string]ServerConfig{}}, nil
	}
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	var node yaml.Node
	if err := decoder.Decode(&node); err != nil {
		return Config{}, fmt.Errorf("decode %q: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return Config{}, fmt.Errorf("decode %q: %w", path, err)
		}
		return Config{}, fmt.Errorf("decode %q: multiple YAML documents are not supported", path)
	}
	if err := expandEnvironment(&node, lookup); err != nil {
		return Config{}, fmt.Errorf("expand %q: %w", path, err)
	}
	var normalized bytes.Buffer
	encoder := yaml.NewEncoder(&normalized)
	if err := encoder.Encode(&node); err != nil {
		return Config{}, fmt.Errorf("encode expanded %q: %w", path, err)
	}
	strict := yaml.NewDecoder(&normalized)
	strict.KnownFields(true)
	var configured Config
	if err := strict.Decode(&configured); err != nil {
		return Config{}, fmt.Errorf("decode %q: %w", path, err)
	}
	if configured.Servers == nil {
		configured.Servers = map[string]ServerConfig{}
	}
	return configured, nil
}

var environmentReference = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*\}`)

func expandEnvironment(node *yaml.Node, lookup config.EnvLookup) error {
	switch node.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range node.Content {
			if err := expandEnvironment(child, lookup); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		for index := 1; index < len(node.Content); index += 2 {
			if err := expandEnvironment(node.Content[index], lookup); err != nil {
				return err
			}
		}
	case yaml.ScalarNode:
		for _, reference := range environmentReference.FindAllString(node.Value, -1) {
			name := reference[2 : len(reference)-1]
			value, ok := lookup(name)
			if !ok {
				return fmt.Errorf("environment variable %s is not set", name)
			}
			node.Value = strings.ReplaceAll(node.Value, reference, value)
		}
	}
	return nil
}

func cloneServer(server ServerConfig) ServerConfig {
	cloned := server
	cloned.Args = append([]string(nil), server.Args...)
	cloned.Env = cloneMap(server.Env)
	cloned.Headers = cloneMap(server.Headers)
	if server.Enabled != nil {
		value := *server.Enabled
		cloned.Enabled = &value
	}
	return cloned
}

func cloneMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func validServerName(value string) bool {
	if value == "" || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}
