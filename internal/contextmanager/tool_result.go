package contextmanager

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Godric-W/Amadeus/internal/imageprep"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type ToolResultError struct {
	Kind    string `json:"kind,omitempty"`
	Message string `json:"message"`
}

type ToolResultPayload struct {
	OK        bool             `json:"ok"`
	Status    string           `json:"status"`
	Text      string           `json:"text,omitempty"`
	Parts     []ToolResultPart `json:"parts,omitempty"`
	Metadata  map[string]any   `json:"metadata,omitempty"`
	Error     *ToolResultError `json:"error,omitempty"`
	Partial   bool             `json:"partial,omitempty"`
	Truncated bool             `json:"truncated,omitempty"`
	Omitted   []string         `json:"omitted_modalities,omitempty"`
}

type ToolResultPart struct {
	Kind      tool.ContentKind `json:"kind"`
	Text      string           `json:"text,omitempty"`
	MediaType string           `json:"media_type,omitempty"`
	Detail    string           `json:"detail,omitempty"`
}

type ToolResultProjection struct {
	CallID   string
	Status   string
	Result   tool.ToolResult
	Error    *ToolResultError
	Partial  bool
	Metadata map[string]any
}

func filterToolResultModalities(content string, model llm.ModelInfo) string {
	var payload ToolResultPayload
	if json.Unmarshal([]byte(content), &payload) != nil {
		return content
	}
	if model.SupportsInput(llm.InputModalityImage) {
		return content
	}
	parts := payload.Parts[:0]
	omittedImage := false
	for _, part := range payload.Parts {
		if part.Kind == tool.ContentImage {
			omittedImage = true
			continue
		}
		parts = append(parts, part)
	}
	payload.Parts = parts
	if omittedImage {
		payload.Omitted = appendUniqueString(payload.Omitted, string(llm.InputModalityImage))
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return content
	}
	return string(encoded)
}

func toolContentPartTokenEstimate(content string, part llm.ContentPart) (int64, bool) {
	var payload ToolResultPayload
	if json.Unmarshal([]byte(content), &payload) == nil {
		width, widthOK := metadataInteger(payload.Metadata, "prepared_width")
		height, heightOK := metadataInteger(payload.Metadata, "prepared_height")
		if widthOK && heightOK && width > 0 && height > 0 {
			return int64(imageprep.PatchCount(width, height)), true
		}
	}
	if strings.TrimSpace(part.Data) == "" {
		return 0, false
	}
	decodedBytes := int64(len(part.Data)) * 3 / 4
	return max(int64(85), (decodedBytes+1023)/1024), true
}

func markToolResultImageOmitted(content, reason, marker string) string {
	var payload ToolResultPayload
	if json.Unmarshal([]byte(content), &payload) != nil {
		return content
	}
	parts := payload.Parts[:0]
	for _, part := range payload.Parts {
		if part.Kind != tool.ContentImage {
			parts = append(parts, part)
		}
	}
	payload.Parts = parts
	payload.Omitted = appendUniqueString(payload.Omitted, reason)
	if !strings.Contains(payload.Text, marker) {
		if strings.TrimSpace(payload.Text) == "" {
			payload.Text = marker
		} else {
			payload.Text = strings.TrimSpace(payload.Text) + "\n\n" + marker
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return content
	}
	return string(encoded)
}

func metadataInteger(metadata map[string]any, key string) (int, bool) {
	value, exists := metadata[key]
	if !exists {
		return 0, false
	}
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), typed == float64(int(typed))
	case json.Number:
		parsed, err := typed.Int64()
		return int(parsed), err == nil
	default:
		return 0, false
	}
}

func appendUniqueString(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func ProjectToolResult(input ToolResultProjection) (llm.ResponseItem, error) {
	callID := strings.TrimSpace(input.CallID)
	if callID == "" {
		return llm.ResponseItem{}, fmt.Errorf("project tool result: call ID is empty")
	}
	result := input.Result.Clone()
	status := normalizeToolResultStatus(input.Status, input.Error)
	metadata := projectToolMetadata(result.Metadata, input.Metadata)
	payload := ToolResultPayload{
		OK: status == "succeeded", Status: status, Text: result.Text,
		Parts: projectToolResultParts(result.Parts), Metadata: metadata,
		Error: cloneToolResultError(input.Error), Partial: input.Partial || result.Partial,
		Truncated: metadataIndicatesTruncation(metadata),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return llm.ResponseItem{}, fmt.Errorf("encode tool result projection: %w", err)
	}
	parts := make([]llm.ContentPart, 0, len(result.Parts))
	for _, part := range result.Parts {
		switch part.Kind {
		case tool.ContentText:
			parts = append(parts, llm.TextPart(part.Text))
		case tool.ContentImage:
			parts = append(parts, llm.ImagePartWithDetail(part.MediaType, part.Data, part.Detail))
		}
	}
	return llm.ToolResultMessageWithParts(callID, string(encoded), parts...), nil
}

func projectToolResultParts(parts []tool.ContentPart) []ToolResultPart {
	result := make([]ToolResultPart, 0, len(parts))
	for _, part := range parts {
		result = append(result, ToolResultPart{Kind: part.Kind, Text: part.Text, MediaType: part.MediaType, Detail: part.Detail})
	}
	return result
}

func truncateToolResultProjection(toolName, content string, maximumTokens int64, estimator Estimator) (string, bool) {
	var payload ToolResultPayload
	if json.Unmarshal([]byte(content), &payload) != nil || strings.TrimSpace(payload.Status) == "" {
		return "", false
	}
	if maximumTokens <= 0 || estimator.EstimateText(content) <= maximumTokens {
		return content, true
	}
	text := payload.Text
	partTexts := make([]string, len(payload.Parts))
	for index := range payload.Parts {
		partTexts[index] = payload.Parts[index].Text
		payload.Parts[index].Text = ""
	}
	payload.Text = ""
	payload.Truncated = true
	overhead, err := json.Marshal(payload)
	if err != nil {
		return "", false
	}
	budget := maximumTokens - estimator.EstimateText(string(overhead))
	textFields := 1
	for _, value := range partTexts {
		if value != "" {
			textFields++
		}
	}
	perField := budget / int64(textFields)
	payload.Text = truncateToolText(toolName, text, perField, estimator)
	for index, value := range partTexts {
		if value != "" {
			payload.Parts[index].Text = truncateToolText(toolName, value, perField, estimator)
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", false
	}
	return string(encoded), true
}

func normalizeToolResultStatus(status string, resultError *ToolResultError) string {
	kind := ""
	if resultError != nil {
		kind = strings.ToLower(strings.TrimSpace(resultError.Kind))
	}
	switch kind {
	case "stale", "stale_file", "read_required":
		return "stale"
	case "permission_denied", "approval_denied", "path_denied":
		return "declined"
	case "cancelled", "canceled", "interrupted":
		return "cancelled"
	}
	switch strings.TrimSpace(status) {
	case "", "completed", "succeeded":
		return "succeeded"
	case "denied", "declined":
		return "declined"
	case "interrupted", "cancelled":
		return "cancelled"
	case "stale":
		return "stale"
	default:
		return "failed"
	}
}

func cloneToolResultError(value *ToolResultError) *ToolResultError {
	if value == nil || strings.TrimSpace(value.Message) == "" {
		return nil
	}
	return &ToolResultError{Kind: strings.TrimSpace(value.Kind), Message: strings.TrimSpace(value.Message)}
}

var modelMetadataKeys = map[string]struct{}{
	"backend": {}, "bytes": {}, "cancelled": {}, "content_type": {}, "exit_code": {},
	"files_searched": {}, "files_skipped": {}, "height": {}, "matches_returned": {},
	"next_line": {}, "operation_count": {}, "origin_call_id": {}, "output_bytes": {},
	"output_truncated": {}, "partial": {}, "path": {}, "pattern": {}, "process_id": {},
	"query": {}, "references": {}, "results": {}, "revision": {}, "source": {},
	"status": {}, "timed_out": {}, "title": {}, "total_matches": {}, "total_operations": {},
	"truncation_reason": {}, "url": {}, "width": {},
	"detail": {}, "source_media_type": {}, "prepared_media_type": {},
	"source_width": {}, "source_height": {}, "prepared_width": {}, "prepared_height": {},
	"source_bytes": {}, "prepared_bytes": {},
	"start_line": {}, "end_line": {}, "total_lines": {}, "lines_truncated": {}, "complete_snapshot": {},
}

func projectToolMetadata(values ...map[string]any) map[string]any {
	result := make(map[string]any)
	for _, value := range values {
		for key, item := range value {
			if _, ok := modelMetadataKeys[key]; !ok {
				continue
			}
			if _, err := json.Marshal(item); err == nil {
				result[key] = item
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func metadataIndicatesTruncation(metadata map[string]any) bool {
	if value, ok := metadata["output_truncated"].(bool); ok && value {
		return true
	}
	value, ok := metadata["truncation_reason"]
	return ok && strings.TrimSpace(fmt.Sprint(value)) != ""
}
