package instruction

import (
	"context"
	"errors"
	"fmt"
)

type LayeredResolver struct {
	user    *UserLoader
	project *ProjectLoader
}

func NewLayeredResolver(user *UserLoader, project *ProjectLoader) (*LayeredResolver, error) {
	if user == nil {
		return nil, errors.New("instruction Resolver user loader is nil")
	}
	if project == nil {
		return nil, errors.New("instruction Resolver project loader is nil")
	}
	return &LayeredResolver{user: user, project: project}, nil
}

func (resolver *LayeredResolver) Resolve(ctx context.Context, request ResolveRequest) (Resolution, error) {
	if resolver == nil || resolver.user == nil || resolver.project == nil {
		return Resolution{}, errors.New("instruction Resolver is nil")
	}
	if err := request.Validate(); err != nil {
		return Resolution{}, err
	}
	if resolver.project.Root().Path() != request.Project.Path() {
		return Resolution{}, fmt.Errorf("instruction Resolver project root %q does not match request %q", resolver.project.Root().Path(), request.Project.Path())
	}
	userDocument, err := resolver.user.Load(ctx)
	if err != nil {
		return Resolution{}, fmt.Errorf("load user instructions: %w", err)
	}
	projectDocuments, err := resolver.project.Discover(ctx, instructionTargetDirectory(request))
	if err != nil {
		return Resolution{}, fmt.Errorf("discover project instructions: %w", err)
	}

	documents := make([]InstructionDocument, 0, len(projectDocuments)+1)
	if userDocument != nil && !containsInstructionPath(projectDocuments, userDocument.Path) {
		documents = append(documents, *userDocument)
	}
	documents = append(documents, projectDocuments...)
	resolution := Resolution{
		TargetPath: request.TargetPath,
		TargetKind: request.TargetKind,
		Documents:  documents,
	}
	if err := resolution.Validate(request); err != nil {
		return Resolution{}, fmt.Errorf("validate instruction resolution: %w", err)
	}
	return resolution, nil
}

func containsInstructionPath(documents []InstructionDocument, path string) bool {
	for _, document := range documents {
		if document.Path == path {
			return true
		}
	}
	return false
}

var _ Resolver = (*LayeredResolver)(nil)
