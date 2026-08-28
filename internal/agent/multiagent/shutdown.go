package multiagent

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Godric-W/Amadeus/internal/protocol"
)

type closeMode uint8

const (
	closeModeRollback closeMode = iota
	closeModeUnload
	closeModeExplicit
)

func (control *Control) closeAgent(ctx context.Context, id protocol.ThreadID, mode closeMode) error {
	ids := control.descendantsAndSelf(id)
	var result error
	for index := len(ids) - 1; index >= 0; index-- {
		result = errors.Join(result, control.closeOne(ctx, ids[index], mode))
	}
	return result
}

func (control *Control) closeOne(ctx context.Context, id protocol.ThreadID, mode closeMode) error {
	control.mu.Lock()
	agent := control.agents[id]
	if agent == nil {
		control.mu.Unlock()
		return nil
	}
	if agent.closing {
		if agent.runtime == nil {
			control.mu.Unlock()
			return nil
		}
		terminated := agent.runtime.Terminated()
		control.mu.Unlock()
		select {
		case <-terminated:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	agent.closing = true
	runtime := agent.runtime
	status := agent.status
	metadata := agent.metadata
	control.mu.Unlock()
	if mode == closeModeExplicit {
		if err := control.host.RecordSpawnEdge(ctx, metadata.ParentThreadID, metadata.ThreadID, protocol.AgentSpawnEdgeClosed); err != nil {
			control.mu.Lock()
			if current := control.agents[id]; current == agent {
				agent.closing = false
				control.signalLocked()
			}
			control.mu.Unlock()
			return fmt.Errorf("persist closed agent edge %q: %w", id, err)
		}
	}
	if runtime == nil {
		control.mu.Lock()
		if current := control.agents[id]; current == agent {
			delete(control.agents, id)
			delete(control.nicknames, agent.metadata.AgentNickname)
			control.signalLocked()
		}
		control.mu.Unlock()
		return nil
	}
	if status.Kind == protocol.AgentStatusRunning {
		_ = runtime.Submit(ctx, protocol.InterruptOp{})
	}
	err := shutdownRuntime(ctx, runtime)
	terminated := false
	select {
	case <-runtime.Terminated():
		terminated = true
	default:
	}
	control.mu.Lock()
	if terminated {
		if current := control.agents[id]; current == agent {
			delete(control.agents, id)
			delete(control.nicknames, agent.metadata.AgentNickname)
			control.signalLocked()
		}
	} else if current := control.agents[id]; current == agent {
		agent.closing = false
		control.signalLocked()
	}
	control.mu.Unlock()
	return err
}

func shutdownRuntime(ctx context.Context, runtime AgentRuntime) error {
	if runtime == nil {
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := runtime.Shutdown(shutdownCtx)
	if err == nil {
		return nil
	}
	select {
	case <-runtime.Terminated():
		return nil
	default:
		return err
	}
}

func (control *Control) descendantsAndSelf(id protocol.ThreadID) []protocol.ThreadID {
	control.mu.Lock()
	defer control.mu.Unlock()
	if control.agents[id] == nil {
		return nil
	}
	result := []protocol.ThreadID{id}
	for changed := true; changed; {
		changed = false
		for childID, child := range control.agents {
			if containsID(result, childID) || !containsID(result, child.metadata.ParentThreadID) {
				continue
			}
			result = append(result, childID)
			changed = true
		}
	}
	return result
}
