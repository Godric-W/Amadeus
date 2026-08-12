package event

import (
	"fmt"
	"reflect"
)

var metadataFieldNames = [...]string{"SessionID", "TurnID", "TaskID", "Iteration", "LLMCallID"}

func MetadataFromEvent(runtimeEvent Event) Metadata {
	value, ok := eventStructValue(runtimeEvent)
	if !ok {
		return Metadata{}
	}
	return Metadata{
		SessionID: stringField(value, "SessionID"),
		TurnID:    stringField(value, "TurnID"),
		TaskID:    stringField(value, "TaskID"),
		Iteration: intField(value, "Iteration"),
		LLMCallID: stringField(value, "LLMCallID"),
	}
}

func WithEventMetadata(runtimeEvent Event, metadata Metadata) (Event, error) {
	if runtimeEvent == nil || isNilEvent(runtimeEvent) {
		return nil, ErrNilEvent
	}
	value := reflect.ValueOf(runtimeEvent)
	pointer := value.Kind() == reflect.Pointer
	if pointer {
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return nil, fmt.Errorf("event %T is not a struct", runtimeEvent)
	}
	copyValue := reflect.New(value.Type()).Elem()
	copyValue.Set(value)
	current := MetadataFromEvent(runtimeEvent)
	merged := mergeMetadata(metadata, current)
	values := map[string]any{
		"SessionID": merged.SessionID,
		"TurnID":    merged.TurnID,
		"TaskID":    merged.TaskID,
		"Iteration": merged.Iteration,
		"LLMCallID": merged.LLMCallID,
	}
	for _, name := range metadataFieldNames {
		field := copyValue.FieldByName(name)
		if !field.IsValid() || !field.CanSet() {
			return nil, fmt.Errorf("event %T is missing writable metadata field %s", runtimeEvent, name)
		}
		switch value := values[name].(type) {
		case string:
			field.SetString(value)
		case int:
			field.SetInt(int64(value))
		}
	}
	if pointer {
		result := reflect.New(copyValue.Type())
		result.Elem().Set(copyValue)
		return result.Interface().(Event), nil
	}
	return copyValue.Interface().(Event), nil
}

func mergeMetadata(base, override Metadata) Metadata {
	if override.SessionID != "" {
		base.SessionID = override.SessionID
	}
	if override.TurnID != "" {
		base.TurnID = override.TurnID
	}
	if override.TaskID != "" {
		base.TaskID = override.TaskID
	}
	if override.Iteration != 0 {
		base.Iteration = override.Iteration
	}
	if override.LLMCallID != "" {
		base.LLMCallID = override.LLMCallID
	}
	return base
}

func eventStructValue(runtimeEvent Event) (reflect.Value, bool) {
	if runtimeEvent == nil || isNilEvent(runtimeEvent) {
		return reflect.Value{}, false
	}
	value := reflect.ValueOf(runtimeEvent)
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	return value, value.Kind() == reflect.Struct
}

func stringField(value reflect.Value, name string) string {
	field := value.FieldByName(name)
	if !field.IsValid() || field.Kind() != reflect.String {
		return ""
	}
	return field.String()
}

func intField(value reflect.Value, name string) int {
	field := value.FieldByName(name)
	if !field.IsValid() || field.Kind() != reflect.Int {
		return 0
	}
	return int(field.Int())
}
