package testutil

import (
	"fmt"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol/identity"
)

func ThreadID(index uint64) identity.ThreadID {
	value := fmt.Sprintf("00000000-0000-7000-8000-%012x", index)
	id, err := identity.ParseThreadID(value)
	if err != nil {
		panic(err)
	}
	return id
}

func BaseInstructions(model string) llm.BaseInstructions {
	return llm.NewModelBaseInstructions("test base instructions", model)
}

func SessionID(index uint64) identity.SessionID {
	return identity.SessionIDFromThreadID(ThreadID(index))
}
