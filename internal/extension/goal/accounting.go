package goal

import (
	"context"
	"sync"
	"time"

	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/protocol"
)

type accountingState struct {
	mu             sync.Mutex
	clock          func() time.Time
	progress       chan struct{}
	current        protocol.TurnID
	turns          map[protocol.TurnID]*turnAccounting
	wall           wallAccounting
	reportedGoalID string
}

type turnAccounting struct {
	currentUsage llm.TokenUsage
	lastUsage    llm.TokenUsage
	goalID       string
	account      bool
}

type wallAccounting struct {
	last   time.Time
	goalID string
}

type progressSnapshot struct {
	currentUsage llm.TokenUsage
	goalID       string
	tokenDelta   int64
	timeDelta    int64
}

func newAccountingState(clock func() time.Time) *accountingState {
	if clock == nil {
		clock = time.Now
	}
	progress := make(chan struct{}, 1)
	progress <- struct{}{}
	return &accountingState{clock: clock, progress: progress, turns: make(map[protocol.TurnID]*turnAccounting), wall: wallAccounting{last: clock()}}
}

func (accounting *accountingState) acquireProgress(ctx context.Context) (func(), error) {
	select {
	case <-accounting.progress:
		return func() { accounting.progress <- struct{}{} }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (accounting *accountingState) startTurn(turnID protocol.TurnID, mode protocol.ModeKind, usage llm.TokenUsage) {
	accounting.mu.Lock()
	defer accounting.mu.Unlock()
	accounting.current = turnID
	accounting.turns[turnID] = &turnAccounting{currentUsage: usage, lastUsage: usage, account: mode != protocol.ModeKindPlan}
}

func (accounting *accountingState) currentTurnID() protocol.TurnID {
	accounting.mu.Lock()
	defer accounting.mu.Unlock()
	return accounting.current
}

func (accounting *accountingState) recordUsage(turnID protocol.TurnID, usage llm.TokenUsage) {
	accounting.mu.Lock()
	defer accounting.mu.Unlock()
	if turn := accounting.turns[turnID]; turn != nil {
		turn.currentUsage = usage
	}
}

func (accounting *accountingState) markTurnGoalActive(turnID protocol.TurnID, goalID string) {
	accounting.mu.Lock()
	defer accounting.mu.Unlock()
	turn := accounting.turns[turnID]
	if turn == nil || !turn.account {
		return
	}
	accounting.resetReportedGoal(goalID)
	turn.goalID = goalID
	if accounting.current == turnID {
		accounting.markWallActive(goalID)
	}
}

func (accounting *accountingState) markCurrentGoalActive(goalID string) protocol.TurnID {
	accounting.mu.Lock()
	defer accounting.mu.Unlock()
	turn := accounting.turns[accounting.current]
	if turn == nil || !turn.account {
		return ""
	}
	accounting.resetReportedGoal(goalID)
	turn.goalID = goalID
	turn.lastUsage = turn.currentUsage
	accounting.markWallActive(goalID)
	return accounting.current
}

func (accounting *accountingState) markIdleGoalActive(goalID string) {
	accounting.mu.Lock()
	defer accounting.mu.Unlock()
	accounting.resetReportedGoal(goalID)
	accounting.markWallActive(goalID)
}

func (accounting *accountingState) currentTurnOwnsGoal(turnID protocol.TurnID) bool {
	accounting.mu.Lock()
	defer accounting.mu.Unlock()
	turn := accounting.turns[turnID]
	return accounting.current == turnID && turn != nil && turn.account && turn.goalID != ""
}

func (accounting *accountingState) snapshot(turnID protocol.TurnID) *progressSnapshot {
	accounting.mu.Lock()
	defer accounting.mu.Unlock()
	turn := accounting.turns[turnID]
	if turn == nil || !turn.account || turn.goalID == "" {
		return nil
	}
	tokenDelta := goalTokenDelta(usageDelta(turn.lastUsage, turn.currentUsage))
	timeDelta := int64(0)
	if accounting.wall.goalID == turn.goalID {
		timeDelta = max(int64(accounting.clock().Sub(accounting.wall.last)/time.Second), 0)
	}
	if tokenDelta <= 0 && timeDelta <= 0 {
		return nil
	}
	return &progressSnapshot{currentUsage: turn.currentUsage, goalID: turn.goalID, tokenDelta: tokenDelta, timeDelta: timeDelta}
}

func (accounting *accountingState) idleSnapshot() *progressSnapshot {
	accounting.mu.Lock()
	defer accounting.mu.Unlock()
	if accounting.wall.goalID == "" {
		return nil
	}
	timeDelta := max(int64(accounting.clock().Sub(accounting.wall.last)/time.Second), 0)
	if timeDelta <= 0 {
		return nil
	}
	return &progressSnapshot{goalID: accounting.wall.goalID, timeDelta: timeDelta}
}

func (accounting *accountingState) markAccounted(turnID protocol.TurnID, snapshot *progressSnapshot, status protocol.ThreadGoalStatus, keepBudgetLimitedActive bool) {
	if snapshot == nil {
		return
	}
	accounting.mu.Lock()
	defer accounting.mu.Unlock()
	clear := status != protocol.ThreadGoalActive && !(status == protocol.ThreadGoalBudgetLimited && keepBudgetLimitedActive)
	if turn := accounting.turns[turnID]; turn != nil {
		turn.lastUsage = snapshot.currentUsage
		if clear {
			turn.goalID = ""
		}
	}
	accounting.advanceWall(snapshot.timeDelta)
	if clear {
		accounting.clearWall()
	}
	if status != protocol.ThreadGoalBudgetLimited {
		accounting.reportedGoalID = ""
	}
}

func (accounting *accountingState) markIdleAccounted(snapshot *progressSnapshot, status protocol.ThreadGoalStatus, keepBudgetLimitedActive bool) {
	if snapshot == nil {
		return
	}
	accounting.mu.Lock()
	defer accounting.mu.Unlock()
	accounting.advanceWall(snapshot.timeDelta)
	if status != protocol.ThreadGoalActive && !(status == protocol.ThreadGoalBudgetLimited && keepBudgetLimitedActive) {
		accounting.clearWall()
	}
}

func (accounting *accountingState) clearCurrentGoal() protocol.TurnID {
	accounting.mu.Lock()
	defer accounting.mu.Unlock()
	if turn := accounting.turns[accounting.current]; turn != nil {
		turn.goalID = ""
	}
	turnID := accounting.current
	accounting.clearWall()
	accounting.reportedGoalID = ""
	return turnID
}

func (accounting *accountingState) clearActiveGoal() {
	accounting.mu.Lock()
	defer accounting.mu.Unlock()
	if turn := accounting.turns[accounting.current]; turn != nil {
		turn.goalID = ""
	}
	accounting.clearWall()
	accounting.reportedGoalID = ""
}

func (accounting *accountingState) finishTurn(turnID protocol.TurnID) {
	accounting.mu.Lock()
	defer accounting.mu.Unlock()
	delete(accounting.turns, turnID)
	if accounting.current == turnID {
		accounting.current = ""
	}
}

func (accounting *accountingState) markBudgetReported(goalID string) bool {
	accounting.mu.Lock()
	defer accounting.mu.Unlock()
	if accounting.reportedGoalID == goalID {
		return false
	}
	accounting.reportedGoalID = goalID
	return true
}

func (accounting *accountingState) markWallActive(goalID string) {
	if accounting.wall.goalID != goalID {
		accounting.wall.goalID = goalID
		accounting.wall.last = accounting.clock()
	}
}

func (accounting *accountingState) advanceWall(seconds int64) {
	if seconds > 0 {
		accounting.wall.last = accounting.wall.last.Add(time.Duration(seconds) * time.Second)
	}
}

func (accounting *accountingState) clearWall() {
	accounting.wall.goalID = ""
	accounting.wall.last = accounting.clock()
}

func (accounting *accountingState) resetReportedGoal(goalID string) {
	if accounting.reportedGoalID != goalID {
		accounting.reportedGoalID = ""
	}
}

func usageDelta(previous, current llm.TokenUsage) llm.TokenUsage {
	return llm.TokenUsage{
		InputTokens:       max(current.InputTokens-previous.InputTokens, 0),
		CachedInputTokens: max(current.CachedInputTokens-previous.CachedInputTokens, 0),
		OutputTokens:      max(current.OutputTokens-previous.OutputTokens, 0),
		ReasoningTokens:   max(current.ReasoningTokens-previous.ReasoningTokens, 0),
		TotalTokens:       max(current.TotalTokens-previous.TotalTokens, 0),
	}
}

func goalTokenDelta(usage llm.TokenUsage) int64 {
	return max(usage.InputTokens-usage.CachedInputTokens, 0) + max(usage.OutputTokens, 0)
}
