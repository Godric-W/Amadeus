package main

import (
	"fmt"
	"sync/atomic"
	"time"
)

var agentRunSequence atomic.Uint64

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

func nextAgentRunID() string {
	return fmt.Sprintf("run-%d-%d", time.Now().UTC().UnixNano(), agentRunSequence.Add(1))
}
