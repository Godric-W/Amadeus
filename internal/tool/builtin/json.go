package builtin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

func decodeArguments(input json.RawMessage, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode tool arguments: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("decode tool arguments: multiple JSON values")
		}
		return fmt.Errorf("decode tool arguments trailing content: %w", err)
	}
	return nil
}
