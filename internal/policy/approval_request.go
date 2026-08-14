package policy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Godric-W/Amadeus/internal/filechange"
)

type ApprovalRequest struct {
	ID              string               `json:"id"`
	ToolName        string               `json:"tool_name"`
	Arguments       json.RawMessage      `json:"arguments"`
	ArgumentsSHA256 string               `json:"arguments_sha256"`
	Purpose         ApprovalPurpose      `json:"purpose"`
	Risk            CommandRisk          `json:"risk"`
	Cause           ApprovalCause        `json:"cause"`
	Path            string               `json:"path,omitempty"`
	Command         string               `json:"command,omitempty"`
	CWD             string               `json:"cwd,omitempty"`
	PermissionKey   string               `json:"permission_key,omitempty"`
	Diff            *filechange.Preview  `json:"diff,omitempty"`
	Presentation    ApprovalPresentation `json:"presentation,omitempty"`
}

func NewApprovalRequest(id, toolName string, arguments json.RawMessage, risk CommandRisk, cause ApprovalCause) (ApprovalRequest, error) {
	return NewApprovalRequestForPurpose(id, toolName, arguments, ApprovalPurposeCommand, risk, cause)
}

func NewApprovalRequestForPurpose(id, toolName string, arguments json.RawMessage, purpose ApprovalPurpose, risk CommandRisk, cause ApprovalCause) (ApprovalRequest, error) {
	canonical, err := canonicalArguments(arguments)
	if err != nil {
		return ApprovalRequest{}, err
	}
	request := ApprovalRequest{
		ID: strings.TrimSpace(id), ToolName: strings.TrimSpace(toolName), Arguments: canonical,
		ArgumentsSHA256: approvalHash(canonical), Purpose: purpose, Risk: risk, Cause: cause,
	}
	if err := request.Validate(); err != nil {
		return ApprovalRequest{}, err
	}
	return request, nil
}

func (request ApprovalRequest) Validate() error {
	if request.ID == "" {
		return errors.New("approval request ID is empty")
	}
	if request.ToolName == "" {
		return errors.New("approval request tool name is empty")
	}
	if request.Purpose != ApprovalPurposeCommand && request.Purpose != ApprovalPurposePermission && request.Purpose != ApprovalPurposeFile && request.Purpose != ApprovalPurposeExternal {
		return fmt.Errorf("approval purpose %q is invalid", request.Purpose)
	}
	if !request.Risk.Valid() {
		return fmt.Errorf("approval request risk %q is invalid", request.Risk)
	}
	if err := request.Cause.Validate(); err != nil {
		return err
	}
	canonical, err := canonicalArguments(request.Arguments)
	if err != nil {
		return err
	}
	if !bytes.Equal(canonical, request.Arguments) {
		return errors.New("approval request arguments are not canonical JSON")
	}
	if request.ArgumentsSHA256 != approvalHash(request.Arguments) {
		return errors.New("approval request arguments SHA-256 does not match")
	}
	return nil
}

func (request ApprovalRequest) Clone() ApprovalRequest {
	request.Arguments = append(json.RawMessage(nil), request.Arguments...)
	request.Presentation.Details = append([]string(nil), request.Presentation.Details...)
	request.Presentation.Options = append([]ApprovalOption(nil), request.Presentation.Options...)
	if request.Diff != nil {
		cloned := request.Diff.Clone()
		request.Diff = &cloned
	}
	return request
}

func canonicalArguments(arguments json.RawMessage) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(arguments))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode approval arguments: %w", err)
	}
	if value == nil {
		return nil, errors.New("decode approval arguments: expected JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("decode approval arguments: multiple JSON values")
		}
		return nil, fmt.Errorf("decode approval arguments trailing content: %w", err)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode canonical approval arguments: %w", err)
	}
	return canonical, nil
}

func approvalHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
