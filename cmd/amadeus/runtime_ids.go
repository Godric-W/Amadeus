package main

import (
	"fmt"
	"sync/atomic"
	"time"
)

var agentTurnSequence atomic.Uint64

func (runner *agentController) runtimeID(kind string) string {
	factory := runner.runtime.persistentIDFactory
	if factory == nil {
		factory = nextPersistentID
	}
	return factory(kind)
}

func (runner *agentController) runtimeNow() time.Time {
	clock := runner.runtime.now
	if clock == nil {
		clock = time.Now
	}
	return clock().UTC()
}

func nextAgentTurnID() string {
	return fmt.Sprintf("turn-%d-%d", time.Now().UTC().UnixNano(), agentTurnSequence.Add(1))
}
