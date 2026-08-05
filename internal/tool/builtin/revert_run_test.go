package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Godric-W/Amadeus/internal/snapshot"
)

type fakeRevertService struct {
	runID  string
	result snapshot.RevertResult
	err    error
}

func (*fakeRevertService) Begin(context.Context, string) (snapshot.Snapshot, error) {
	return snapshot.Snapshot{}, nil
}

func (*fakeRevertService) Complete(context.Context, string) (snapshot.Snapshot, error) {
	return snapshot.Snapshot{}, nil
}

func (service *fakeRevertService) Revert(_ context.Context, runID string) (snapshot.RevertResult, error) {
	service.runID = runID
	return service.result, service.err
}

func TestRevertRunUsesRunIdentityAndReturnsMetadata(t *testing.T) {
	service := &fakeRevertService{result: snapshot.RevertResult{RunID: "run-1", Changes: []snapshot.FileChange{{Path: "a.txt", Kind: snapshot.ChangeModified}}}}
	revert, err := NewRevertRun(service)
	if err != nil {
		t.Fatal(err)
	}
	result, err := revert.Execute(context.Background(), json.RawMessage(`{"run_id":" run-1 "}`))
	if err != nil || service.runID != "run-1" || result.ToolName != "revert_run" || result.Metadata["run_id"] != "run-1" || result.Metadata["changed_files"] != 1 {
		t.Fatalf("unexpected revert result: service=%#v result=%#v err=%v", service, result, err)
	}
}

func TestRevertRunRejectsEmptyAndPropagatesFailure(t *testing.T) {
	expected := errors.New("snapshot unavailable")
	service := &fakeRevertService{err: expected}
	revert, err := NewRevertRun(service)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := revert.Execute(context.Background(), json.RawMessage(`{"run_id":" "}`)); err == nil {
		t.Fatal("empty run ID was accepted")
	}
	if _, err := revert.Execute(context.Background(), json.RawMessage(`{"run_id":"run-2"}`)); !errors.Is(err, expected) {
		t.Fatalf("unexpected revert failure: %v", err)
	}
}
