package prompt

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

type AssembleInput struct {
	Layers    []string
	Variables map[string]string
}

type Bundle struct {
	Content           string   `json:"content"`
	Sources           []Source `json:"sources"`
	RequiredVariables []string `json:"required_variables,omitempty"`
	SHA256            string   `json:"sha256"`
}

type MissingLayersError struct {
	Layers []string
}

func (err *MissingLayersError) Error() string {
	return "missing prompt layers: " + strings.Join(err.Layers, ", ")
}

type VariableError struct {
	Missing []string
	Unknown []string
}

func (err *VariableError) Error() string {
	parts := make([]string, 0, 2)
	if len(err.Missing) != 0 {
		parts = append(parts, "missing: "+strings.Join(err.Missing, ", "))
	}
	if len(err.Unknown) != 0 {
		parts = append(parts, "unknown: "+strings.Join(err.Unknown, ", "))
	}
	return "prompt variables are invalid (" + strings.Join(parts, "; ") + ")"
}

type Assembler struct {
	repository *Repository
}

func NewAssembler(repository *Repository) (*Assembler, error) {
	if repository == nil {
		return nil, errors.New("prompt assembler repository is nil")
	}
	return &Assembler{repository: repository}, nil
}

func (assembler *Assembler) Assemble(input AssembleInput) (Bundle, error) {
	if assembler == nil || assembler.repository == nil {
		return Bundle{}, errors.New("prompt assembler is nil")
	}
	if len(input.Layers) == 0 {
		return Bundle{}, errors.New("prompt assembly layers are empty")
	}

	documents := make([]Document, 0, len(input.Layers))
	missing := make([]string, 0)
	seenLayers := make(map[string]struct{}, len(input.Layers))
	for _, layer := range input.Layers {
		layer = strings.TrimSpace(layer)
		if layer == "" {
			return Bundle{}, errors.New("prompt assembly contains an empty layer")
		}
		if _, ok := seenLayers[layer]; ok {
			return Bundle{}, fmt.Errorf("prompt assembly layer %q is duplicated", layer)
		}
		seenLayers[layer] = struct{}{}
		document, err := assembler.repository.Load(layer)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				missing = append(missing, layer)
				continue
			}
			return Bundle{}, err
		}
		documents = append(documents, document)
	}
	if len(missing) != 0 {
		return Bundle{}, &MissingLayersError{Layers: missing}
	}

	required := requiredVariables(documents)
	missingVariables, unknownVariables := validateVariables(required, input.Variables)
	if len(missingVariables) != 0 || len(unknownVariables) != 0 {
		return Bundle{}, &VariableError{Missing: missingVariables, Unknown: unknownVariables}
	}

	parts := make([]string, len(documents))
	sources := make([]Source, len(documents))
	for index, document := range documents {
		parts[index] = render(document.Content, input.Variables)
		sources[index] = cloneSource(document.Source)
	}
	content := strings.Join(parts, "\n\n")
	return Bundle{
		Content:           content,
		Sources:           sources,
		RequiredVariables: append([]string(nil), required...),
		SHA256:            contentHash(content),
	}, nil
}

func requiredVariables(documents []Document) []string {
	unique := make(map[string]struct{})
	for _, document := range documents {
		for _, variable := range document.Source.Variables {
			unique[variable] = struct{}{}
		}
	}
	result := make([]string, 0, len(unique))
	for variable := range unique {
		result = append(result, variable)
	}
	sort.Strings(result)
	return result
}

func validateVariables(required []string, provided map[string]string) ([]string, []string) {
	requiredSet := make(map[string]struct{}, len(required))
	missing := make([]string, 0)
	for _, variable := range required {
		requiredSet[variable] = struct{}{}
		if _, ok := provided[variable]; !ok {
			missing = append(missing, variable)
		}
	}
	unknown := make([]string, 0)
	for variable := range provided {
		if _, ok := requiredSet[variable]; !ok {
			unknown = append(unknown, variable)
		}
	}
	sort.Strings(unknown)
	return missing, unknown
}

func render(content string, variables map[string]string) string {
	return variablePattern.ReplaceAllStringFunc(content, func(placeholder string) string {
		match := variablePattern.FindStringSubmatch(placeholder)
		return variables[match[1]]
	})
}

func cloneSource(source Source) Source {
	source.Variables = append([]string(nil), source.Variables...)
	return source
}
