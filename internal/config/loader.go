package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

const configFileName = "config.yaml"

type Loader struct {
	path      string
	required  bool
	lookupEnv EnvLookup
}

func NewLoader(amadeusRoot string) Loader {
	if strings.TrimSpace(amadeusRoot) == "" {
		return Loader{}
	}

	return Loader{
		path:      filepath.Join(amadeusRoot, configFileName),
		lookupEnv: os.LookupEnv,
	}
}

func NewFileLoader(path string) Loader {
	return Loader{
		path:      path,
		required:  true,
		lookupEnv: os.LookupEnv,
	}
}

func (loader Loader) WithEnvLookup(lookup EnvLookup) Loader {
	loader.lookupEnv = lookup
	return loader
}

func (loader Loader) ConfigPath() string {
	return loader.path
}

func (loader Loader) Load() (Config, error) {
	if strings.TrimSpace(loader.path) == "" {
		return Config{}, errors.New("config path is empty")
	}

	lookupEnv := loader.lookupEnv
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}

	path := loader.ConfigPath()
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) && !loader.required {
		return applyEnvironmentOverrides(Default(), lookupEnv), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("open config %q: %w", path, err)
	}
	defer file.Close()

	configured, err := decodeAndApply(Default(), file, lookupEnv)
	if err != nil {
		return Config{}, fmt.Errorf("decode config %q: %w", path, err)
	}

	return applyEnvironmentOverrides(configured, lookupEnv), nil
}

func decodeAndApply(base Config, reader io.Reader, lookupEnv EnvLookup) (Config, error) {
	decoder := yaml.NewDecoder(reader)

	var document yaml.Node
	if err := decoder.Decode(&document); errors.Is(err, io.EOF) {
		return clone(base), nil
	} else if err != nil {
		return Config{}, err
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return Config{}, err
		}
		return Config{}, errors.New("multiple YAML documents are not supported")
	}

	if len(document.Content) == 0 {
		return clone(base), nil
	}
	if err := expandEnvironment(document.Content[0], nil, lookupEnv); err != nil {
		return Config{}, err
	}

	expanded, err := yaml.Marshal(document.Content[0])
	if err != nil {
		return Config{}, fmt.Errorf("encode expanded YAML: %w", err)
	}

	strictDecoder := yaml.NewDecoder(bytes.NewReader(expanded))
	strictDecoder.KnownFields(true)
	var patch configPatch
	if err := strictDecoder.Decode(&patch); err != nil {
		return Config{}, err
	}

	configured := patch.apply(base)
	if configured.Version != CurrentVersion {
		return Config{}, fmt.Errorf("config version is %d, expected %d", configured.Version, CurrentVersion)
	}
	return configured, nil
}
