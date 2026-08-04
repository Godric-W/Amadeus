package plan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/Godric-W/Amadeus/internal/agent/event"
	"github.com/Godric-W/Amadeus/internal/llm"
	"github.com/Godric-W/Amadeus/internal/tool"
)

type PlanDraft struct {
	Tasks []string  `json:"tasks"`
	Usage llm.Usage `json:"-"`
}

func ParsePlanDraft(content string) (PlanDraft, error) {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	tasks := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.EqualFold(strings.TrimSuffix(line, ":"), "plan") {
			continue
		}
		if task, ok := planListItem(line); ok {
			tasks = append(tasks, task)
		}
	}
	if len(tasks) == 0 {
		fallback := strings.TrimSpace(content)
		if fallback == "" || strings.EqualFold(strings.TrimSuffix(fallback, ":"), "plan") {
			return PlanDraft{}, errors.New("plan response is empty")
		}
		tasks = append(tasks, fallback)
	}
	return PlanDraft{Tasks: tasks}, nil
}

func planListItem(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
		item := strings.TrimSpace(trimmed[2:])
		return item, item != ""
	}
	position := 0
	for position < len(trimmed) && trimmed[position] >= '0' && trimmed[position] <= '9' {
		position++
	}
	if position > 0 && position+1 < len(trimmed) && (trimmed[position] == '.' || trimmed[position] == ')') && trimmed[position+1] == ' ' {
		item := strings.TrimSpace(trimmed[position+2:])
		return item, item != ""
	}
	return "", false
}

func BuildSerialGraph(draft PlanDraft, version int) (ExecutionGraph, error) {
	if version <= 0 {
		version = 1
	}
	if len(draft.Tasks) == 0 {
		return ExecutionGraph{}, errors.New("plan draft contains no tasks")
	}
	graph := ExecutionGraph{Kind: ExecutionPlanned, Version: version, Tasks: make([]Task, 0, len(draft.Tasks))}
	for index, objective := range draft.Tasks {
		objective = strings.TrimSpace(objective)
		if objective == "" {
			return ExecutionGraph{}, fmt.Errorf("plan task %d is empty", index+1)
		}
		task := Task{ID: TaskID(fmt.Sprintf("task-%d", index+1)), Objective: objective, Status: TaskStatusPending}
		if index > 0 {
			task.Dependencies = []TaskID{graph.Tasks[index-1].ID}
		}
		graph.Tasks = append(graph.Tasks, task)
	}
	if err := graph.Validate(); err != nil {
		return ExecutionGraph{}, err
	}
	return graph, nil
}

type DraftPlanRequest struct {
	Goal      Goal          `json:"goal"`
	Messages  []llm.Message `json:"-"`
	Workspace string        `json:"workspace,omitempty"`
	Budget    BudgetState   `json:"-"`
}

type DraftPlanner interface {
	Draft(context.Context, DraftPlanRequest) (PlanDraft, error)
}

type PlannerOptions struct {
	SystemPrompt    string
	MaxAttempts     int
	Temperature     float64
	MaxOutputTokens int
	Events          event.Sink
}

type LLMPlanDraftPlanner struct {
	client  llm.Client
	options PlannerOptions
	events  event.Sink
}

func NewLLMPlanDraftPlanner(client llm.Client, options PlannerOptions) (*LLMPlanDraftPlanner, error) {
	if client == nil {
		return nil, errors.New("plan draft client is nil")
	}
	if options.MaxAttempts <= 0 {
		options.MaxAttempts = 2
	}
	if strings.TrimSpace(options.SystemPrompt) == "" {
		options.SystemPrompt = "You are Amadeus Planner. Return PLAN followed by a short ordered list of tasks. Do not return JSON or hidden reasoning."
	}
	return &LLMPlanDraftPlanner{client: client, options: options, events: options.Events}, nil
}

func (planner *LLMPlanDraftPlanner) Draft(ctx context.Context, input DraftPlanRequest) (PlanDraft, error) {
	if planner == nil || planner.client == nil {
		return PlanDraft{}, errors.New("plan draft planner is nil")
	}
	if ctx == nil {
		return PlanDraft{}, errors.New("plan draft context is nil")
	}
	if strings.TrimSpace(input.Goal.Objective) == "" {
		return PlanDraft{}, errors.New("plan draft objective is empty")
	}
	payload, err := json.Marshal(struct {
		Objective string `json:"objective"`
		Workspace string `json:"workspace,omitempty"`
	}{Objective: input.Goal.Objective, Workspace: input.Workspace})
	if err != nil {
		return PlanDraft{}, fmt.Errorf("encode plan draft request: %w", err)
	}
	messages := []llm.Message{llm.SystemMessage(planner.options.SystemPrompt)}
	messages = append(messages, input.Messages...)
	messages = append(messages, llm.UserMessage(string(payload)))
	var lastErr error
	for attempt := 0; attempt < planner.options.MaxAttempts; attempt++ {
		callCtx := event.WithMetadata(ctx, event.Metadata{LLMCallID: planLLMCallID(ctx, "initial", attempt+1)})
		if err := publishPlanLLMStarted(callCtx, planner.events, planner.client.Model()); err != nil {
			return PlanDraft{}, err
		}
		response, requestErr := planner.client.Complete(callCtx, llm.Request{
			Model: planner.client.Model().Name, Messages: messages,
			Temperature: planner.options.Temperature, MaxOutputTokens: remainingOutputTokens(planner.options.MaxOutputTokens, input.Budget),
		})
		if requestErr != nil {
			_ = publishPlanLLMError(callCtx, planner.events, requestErr)
			return PlanDraft{}, fmt.Errorf("plan draft provider request: %w", requestErr)
		}
		if err := publishPlanLLMCompleted(callCtx, planner.events, response); err != nil {
			return PlanDraft{}, err
		}
		draft, parseErr := ParsePlanDraft(response.Message.Content)
		if parseErr == nil {
			draft.Usage = response.Usage
			return normalizePlanForObjective(input.Goal.Objective, draft), nil
		}
		lastErr = parseErr
		messages = append(messages, response.Message, llm.UserMessage("The plan was empty. Return PLAN followed by at least one concise task."))
	}
	return PlanDraft{}, fmt.Errorf("plan draft attempts exhausted: %w", lastErr)
}

func normalizePlanForObjective(objective string, draft PlanDraft) PlanDraft {
	if !isConversationalGreeting(objective) {
		return draft
	}
	draft.Tasks = []string{fmt.Sprintf("Respond directly and briefly to the user's message without calling tools: %s", strings.TrimSpace(objective))}
	return draft
}

func isConversationalGreeting(objective string) bool {
	normalized := strings.ToLower(strings.TrimSpace(objective))
	normalized = strings.TrimFunc(normalized, func(character rune) bool {
		return unicode.IsPunct(character) || unicode.IsSymbol(character) || unicode.IsSpace(character)
	})
	switch normalized {
	case "你好", "您好", "你好啊", "您好啊", "嗨", "哈喽", "早上好", "上午好", "下午好", "晚上好",
		"hello", "hello there", "hi", "hi there", "hey", "hey there":
		return true
	default:
		return false
	}
}

type ReplanAction string

const (
	ReplanComplete ReplanAction = "complete"
	ReplanAgain    ReplanAction = "replan"
)

type ReplanDecision struct {
	Action      ReplanAction `json:"action"`
	FinalAnswer string       `json:"final_answer,omitempty"`
	Plan        PlanDraft    `json:"plan,omitempty"`
	Usage       llm.Usage    `json:"-"`
}

func ParseReplanDecision(content string) (ReplanDecision, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return ReplanDecision{}, errors.New("replan response is empty")
	}
	first, rest, found := strings.Cut(content, "\n")
	header := strings.TrimSpace(first)
	upper := strings.ToUpper(header)
	if strings.HasPrefix(upper, "COMPLETE") {
		answer := strings.TrimSpace(strings.TrimPrefix(header, header[:len("COMPLETE")]))
		if found && strings.TrimSpace(rest) != "" {
			if answer != "" {
				answer += "\n"
			}
			answer += strings.TrimSpace(rest)
		}
		if strings.HasPrefix(answer, ":") {
			answer = strings.TrimSpace(strings.TrimPrefix(answer, ":"))
		}
		if answer == "" {
			return ReplanDecision{}, errors.New("COMPLETE response has no final answer")
		}
		return ReplanDecision{Action: ReplanComplete, FinalAnswer: answer}, nil
	}
	if strings.HasPrefix(upper, "REPLAN") {
		body := strings.TrimSpace(rest)
		if body == "" && len(header) > len("REPLAN") {
			body = strings.TrimSpace(header[len("REPLAN"):])
		}
		draft, err := ParsePlanDraft(body)
		if err != nil {
			return ReplanDecision{}, err
		}
		return ReplanDecision{Action: ReplanAgain, Plan: draft}, nil
	}
	return ReplanDecision{}, fmt.Errorf("replan response must start with COMPLETE or REPLAN")
}

type ReplanRequest struct {
	Goal      Goal           `json:"goal"`
	Graph     ExecutionGraph `json:"graph"`
	Evidence  []Evidence     `json:"evidence,omitempty"`
	Workspace string         `json:"workspace,omitempty"`
	LastError string         `json:"last_error,omitempty"`
	Budget    BudgetState    `json:"-"`
}

type FixedReplanner interface {
	Decide(context.Context, ReplanRequest) (ReplanDecision, error)
}

type LLMReplanner struct {
	client  llm.Client
	options PlannerOptions
	events  event.Sink
}

func NewLLMReplanner(client llm.Client, options PlannerOptions) (*LLMReplanner, error) {
	if client == nil {
		return nil, errors.New("replanner client is nil")
	}
	if options.MaxAttempts <= 0 {
		options.MaxAttempts = 2
	}
	if strings.TrimSpace(options.SystemPrompt) == "" {
		options.SystemPrompt = "You are Amadeus Replanner. Return COMPLETE followed by the final user answer when the goal is done, otherwise return REPLAN followed by a short ordered task list. Do not return JSON or hidden reasoning."
	}
	return &LLMReplanner{client: client, options: options, events: options.Events}, nil
}

func (replanner *LLMReplanner) Decide(ctx context.Context, input ReplanRequest) (ReplanDecision, error) {
	if replanner == nil || replanner.client == nil {
		return ReplanDecision{}, errors.New("replanner is nil")
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return ReplanDecision{}, fmt.Errorf("encode replan request: %w", err)
	}
	messages := []llm.Message{llm.SystemMessage(replanner.options.SystemPrompt), llm.UserMessage(string(payload))}
	var lastErr error
	for attempt := 0; attempt < replanner.options.MaxAttempts; attempt++ {
		callCtx := event.WithMetadata(ctx, event.Metadata{LLMCallID: planLLMCallID(ctx, "review", attempt+1)})
		if err := publishPlanLLMStarted(callCtx, replanner.events, replanner.client.Model()); err != nil {
			return ReplanDecision{}, err
		}
		response, requestErr := replanner.client.Complete(callCtx, llm.Request{
			Model: replanner.client.Model().Name, Messages: messages,
			Temperature: replanner.options.Temperature, MaxOutputTokens: remainingOutputTokens(replanner.options.MaxOutputTokens, input.Budget),
		})
		if requestErr != nil {
			_ = publishPlanLLMError(callCtx, replanner.events, requestErr)
			return ReplanDecision{}, fmt.Errorf("replanner provider request: %w", requestErr)
		}
		if err := publishPlanLLMCompleted(callCtx, replanner.events, response); err != nil {
			return ReplanDecision{}, err
		}
		decision, parseErr := ParseReplanDecision(response.Message.Content)
		if parseErr == nil {
			decision.Usage = response.Usage
			return decision, nil
		}
		lastErr = parseErr
		messages = append(messages, response.Message, llm.UserMessage("Return exactly COMPLETE plus a final answer, or REPLAN plus at least one task."))
	}
	return ReplanDecision{}, fmt.Errorf("replanner attempts exhausted: %w", lastErr)
}

type ControllerOptions struct {
	MaxPlanCycles int
	Events        event.Sink
}

type PlanRunInput struct {
	State             RunState                              `json:"state"`
	WorkspaceSnapshot func(context.Context) (string, error) `json:"-"`
	Messages          []llm.Message                         `json:"messages,omitempty"`
	AvailableTools    []tool.Spec                           `json:"available_tools,omitempty"`
}

type PlanRunResult struct {
	State        RunState     `json:"state"`
	FinalMessage *llm.Message `json:"final_message,omitempty"`
	Reason       string       `json:"reason,omitempty"`
}

type Controller struct {
	planner   DraftPlanner
	runner    TaskRunner
	replanner FixedReplanner
	events    event.Sink
	options   ControllerOptions
}

type PlanController interface {
	Run(context.Context, PlanRunInput) (PlanRunResult, error)
}

func NewController(planner DraftPlanner, runner TaskRunner, replanner FixedReplanner, options ControllerOptions) (*Controller, error) {
	if planner == nil {
		return nil, errors.New("plan execute planner is nil")
	}
	if runner == nil {
		return nil, errors.New("plan execute task runner is nil")
	}
	if replanner == nil {
		return nil, errors.New("plan execute replanner is nil")
	}
	if options.MaxPlanCycles <= 0 {
		options.MaxPlanCycles = 8
	}
	return &Controller{planner: planner, runner: runner, replanner: replanner, events: options.Events, options: options}, nil
}

func (controller *Controller) Run(ctx context.Context, input PlanRunInput) (PlanRunResult, error) {
	if controller == nil {
		return PlanRunResult{}, errors.New("plan Controller is nil")
	}
	if ctx == nil {
		return PlanRunResult{}, errors.New("plan execute context is nil")
	}
	if strings.TrimSpace(input.State.Goal.Objective) == "" {
		return PlanRunResult{}, errors.New("plan execute objective is empty")
	}
	result := PlanRunResult{State: input.State}
	if err := controller.publish(ctx, event.RunStarted{RunID: string(result.State.ID)}); err != nil {
		return result, err
	}
	previousStatus := result.State.Status
	result.State.Status = RunStatusPlanning
	if err := controller.publish(ctx, event.RunStatusChanged{RunID: string(result.State.ID), Entity: "run", EntityID: string(result.State.ID), From: string(previousStatus), To: string(result.State.Status)}); err != nil {
		return result, err
	}
	workspace, err := planWorkspace(ctx, input.WorkspaceSnapshot)
	if err != nil {
		return controller.fail(result, StopReasonToolError, err)
	}
	if err := planBudgetAvailable(result.State.Budget); err != nil {
		return controller.fail(result, StopReasonBudgetExceeded, err)
	}
	draft, err := controller.planner.Draft(ctx, DraftPlanRequest{Goal: result.State.Goal, Messages: cloneLLMMessages(input.Messages), Workspace: workspace, Budget: result.State.Budget})
	if err != nil {
		return controller.fail(result, StopReasonProviderError, err)
	}
	result.State.Budget = addLLMUsage(result.State.Budget, draft.Usage)
	if err := planBudgetAvailable(result.State.Budget); err != nil {
		return controller.fail(result, StopReasonBudgetExceeded, err)
	}
	graph, err := BuildSerialGraph(draft, 1)
	if err != nil {
		return controller.fail(result, StopReasonProviderError, err)
	}

	for cycle := 1; cycle <= controller.options.MaxPlanCycles; cycle++ {
		result.State.Graph = graph
		if err := controller.publish(ctx, event.PlanUpdated{RunID: string(result.State.ID), Cycle: cycle, Tasks: planEventTasks(graph)}); err != nil {
			return result, fmt.Errorf("publish plan updated: %w", err)
		}
		lastError, cancelled, runErr := controller.executeGraph(ctx, input, &result)
		if runErr != nil {
			return result, runErr
		}
		if cancelled {
			result.State.Status = RunStatusCancelled
			result.State.StopReason = StopReasonCancelled
			if err := controller.publish(context.WithoutCancel(ctx), event.RunCompleted{
				RunID: string(result.State.ID), Status: string(result.State.Status), StopReason: string(result.State.StopReason), Reason: lastError,
			}); err != nil {
				return result, fmt.Errorf("publish cancelled run completion: %w", err)
			}
			return result, nil
		}
		workspace, err = planWorkspace(ctx, input.WorkspaceSnapshot)
		if err != nil {
			return controller.fail(result, StopReasonToolError, err)
		}
		result.State.Status = RunStatusPlanning
		if err := planBudgetAvailable(result.State.Budget); err != nil {
			return controller.fail(result, StopReasonBudgetExceeded, err)
		}
		reviewCtx := event.WithMetadata(ctx, event.Metadata{Iteration: cycle})
		decision, err := controller.replanner.Decide(reviewCtx, ReplanRequest{
			Goal: result.State.Goal, Graph: result.State.Graph,
			Evidence: cloneEvidence(result.State.Evidence), Workspace: workspace, LastError: lastError, Budget: result.State.Budget,
		})
		if err != nil {
			return controller.fail(result, StopReasonProviderError, err)
		}
		result.State.Budget = addLLMUsage(result.State.Budget, decision.Usage)
		if err := planBudgetAvailable(result.State.Budget); err != nil {
			return controller.fail(result, StopReasonBudgetExceeded, err)
		}
		switch decision.Action {
		case ReplanComplete:
			message := llm.AssistantMessage(decision.FinalAnswer)
			result.FinalMessage = &message
			if err := controller.publish(ctx, event.TextDelta{LLMCallID: string(result.State.ID), ResponseID: "replan-final", Delta: decision.FinalAnswer}); err != nil {
				return result, fmt.Errorf("publish final replanner answer: %w", err)
			}
			result.State.Status = RunStatusCompleted
			result.State.StopReason = StopReasonCompleted
			if err := controller.publish(context.WithoutCancel(ctx), event.RunCompleted{RunID: string(result.State.ID), Status: string(result.State.Status), StopReason: string(result.State.StopReason), Reason: decision.FinalAnswer}); err != nil {
				return result, err
			}
			return result, nil
		case ReplanAgain:
			graph, err = BuildSerialGraph(decision.Plan, cycle+1)
			if err != nil {
				return controller.fail(result, StopReasonProviderError, err)
			}
		default:
			return controller.fail(result, StopReasonProviderError, fmt.Errorf("unsupported replan action %q", decision.Action))
		}
	}
	return controller.fail(result, StopReasonReplanExhausted, errors.New("plan execute cycles exhausted"))
}

func (controller *Controller) executeGraph(ctx context.Context, input PlanRunInput, result *PlanRunResult) (string, bool, error) {
	result.State.Status = RunStatusScheduling
	for index := range result.State.Graph.Tasks {
		if err := ctx.Err(); err != nil {
			return err.Error(), true, nil
		}
		task := &result.State.Graph.Tasks[index]
		if !dependenciesCompleted(result.State.Graph, *task) {
			if err := controller.transitionPlanTask(ctx, result.State.ID, task, TaskStatusBlocked); err != nil {
				return err.Error(), false, err
			}
			return fmt.Sprintf("task %s dependencies are incomplete", task.ID), false, nil
		}
		if err := controller.transitionPlanTask(ctx, result.State.ID, task, TaskStatusRunning); err != nil {
			return err.Error(), false, err
		}
		result.State.ActiveTaskID = task.ID
		result.State.Status = RunStatusTaskRunning
		messages := cloneLLMMessages(input.Messages)
		messages = append(messages, llm.DeveloperMessage(fmt.Sprintf("Execute only the current planned task: %s. Use the available tools as needed. Do not create another plan or request a planning-mode upgrade. Return a concise task result when this task is complete.", task.Objective)))
		outcome, err := controller.runner.Run(ctx, TaskRunInput{
			RunID: result.State.ID, Task: *task, Messages: messages, AvailableTools: cloneToolSpecs(input.AvailableTools),
			Evidence: cloneEvidence(result.State.Evidence), Budget: result.State.Budget,
		})
		if err != nil {
			return err.Error(), false, err
		}
		result.State.Evidence = mergeEvidence(result.State.Evidence, outcome.Evidence)
		result.State.Budget = mergeBudget(result.State.Budget, outcome.Budget)
		switch outcome.Kind {
		case TaskOutcomeCandidateComplete:
			if err := controller.transitionPlanTask(ctx, result.State.ID, task, TaskStatusCompleted); err != nil {
				return err.Error(), false, err
			}
			task.Result = cloneTaskResult(&outcome.Candidate.Result)
		case TaskOutcomeCancelled:
			if err := controller.transitionPlanTask(ctx, result.State.ID, task, TaskStatusCancelled); err != nil {
				return err.Error(), false, err
			}
			return nonEmptyPlanReason(outcome.Reason, "task cancelled"), true, nil
		case TaskOutcomeBlocked:
			if err := controller.transitionPlanTask(ctx, result.State.ID, task, TaskStatusBlocked); err != nil {
				return err.Error(), false, err
			}
			return nonEmptyPlanReason(outcome.Reason, "task blocked"), false, nil
		case TaskOutcomeFailed:
			if err := controller.transitionPlanTask(ctx, result.State.ID, task, TaskStatusFailed); err != nil {
				return err.Error(), false, err
			}
			return nonEmptyPlanReason(outcome.Reason, string(outcome.StopReason)), false, nil
		default:
			return "", false, fmt.Errorf("unsupported task outcome %q", outcome.Kind)
		}
	}
	result.State.ActiveTaskID = ""
	return "", false, nil
}

func (controller *Controller) transitionPlanTask(ctx context.Context, runID RunID, task *Task, next TaskStatus) error {
	if task == nil {
		return errors.New("plan task is nil")
	}
	previous := task.Status
	if previous == next {
		return nil
	}
	task.Status = next
	if err := controller.publish(context.WithoutCancel(ctx), event.RunStatusChanged{
		RunID: string(runID), TaskID: string(task.ID), Entity: "task", EntityID: string(task.ID), From: string(previous), To: string(next),
	}); err != nil {
		return fmt.Errorf("publish task status changed: %w", err)
	}
	return nil
}

func planEventTasks(graph ExecutionGraph) []event.PlanTask {
	tasks := make([]event.PlanTask, 0, len(graph.Tasks))
	for _, task := range graph.Tasks {
		tasks = append(tasks, event.PlanTask{ID: string(task.ID), Objective: task.Objective, Status: string(task.Status)})
	}
	return tasks
}

func addLLMUsage(budget BudgetState, usage llm.Usage) BudgetState {
	budget.InputTokensUsed += usage.InputTokens
	budget.OutputTokensUsed += usage.OutputTokens
	return budget
}

func planBudgetAvailable(budget BudgetState) error {
	if budget.Budget.MaxInputTokens > 0 && budget.InputTokensUsed >= budget.Budget.MaxInputTokens {
		return fmt.Errorf("input token budget reached: used %d of %d", budget.InputTokensUsed, budget.Budget.MaxInputTokens)
	}
	if budget.Budget.MaxOutputTokens > 0 && budget.OutputTokensUsed >= budget.Budget.MaxOutputTokens {
		return fmt.Errorf("output token budget reached: used %d of %d", budget.OutputTokensUsed, budget.Budget.MaxOutputTokens)
	}
	return nil
}

func nonEmptyPlanReason(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return "planned task did not complete"
}

func remainingOutputTokens(configured int, budget BudgetState) int {
	if configured <= 0 || budget.Budget.MaxOutputTokens <= 0 {
		return configured
	}
	remaining := budget.Budget.MaxOutputTokens - budget.OutputTokensUsed
	if remaining <= 0 {
		return 1
	}
	if remaining < int64(configured) {
		return int(remaining)
	}
	return configured
}

func (controller *Controller) fail(result PlanRunResult, reason StopReason, err error) (PlanRunResult, error) {
	result.State.Status = RunStatusFailed
	result.State.StopReason = reason
	result.Reason = err.Error()
	_ = controller.publish(context.Background(), event.RunCompleted{RunID: string(result.State.ID), Status: string(result.State.Status), StopReason: string(reason), Reason: err.Error()})
	return result, err
}

func (controller *Controller) publish(ctx context.Context, runtimeEvent event.Event) error {
	if controller == nil || controller.events == nil {
		return nil
	}
	return controller.events.Publish(ctx, runtimeEvent)
}

func planWorkspace(ctx context.Context, snapshot func(context.Context) (string, error)) (string, error) {
	if snapshot == nil {
		return "", nil
	}
	return snapshot(ctx)
}

func planLLMCallID(ctx context.Context, phase string, attempt int) string {
	metadata := event.MetadataFromContext(ctx)
	runID := metadata.RunID
	if runID == "" {
		runID = "plan"
	}
	if metadata.Iteration > 0 {
		return fmt.Sprintf("%s/plan/%s-cycle-%d-attempt-%d", runID, phase, metadata.Iteration, attempt)
	}
	return fmt.Sprintf("%s/plan/%s-attempt-%d", runID, phase, attempt)
}

func publishPlanLLMStarted(ctx context.Context, sink event.Sink, model llm.ModelInfo) error {
	if sink == nil {
		return nil
	}
	if err := sink.Publish(ctx, event.LLMCallStarted{Model: model}); err != nil {
		return fmt.Errorf("publish Plan LLM call started: %w", err)
	}
	return nil
}

func publishPlanLLMCompleted(ctx context.Context, sink event.Sink, response llm.Response) error {
	if sink == nil {
		return nil
	}
	if err := sink.Publish(ctx, event.UsageUpdated{ResponseID: response.ID, Usage: response.Usage}); err != nil {
		return fmt.Errorf("publish Plan LLM usage: %w", err)
	}
	if err := sink.Publish(ctx, event.LLMCallCompleted{
		ResponseID: response.ID, RequestID: response.RequestID,
		FinishReason: response.FinishReason, ProviderFinishReason: response.ProviderFinishReason,
	}); err != nil {
		return fmt.Errorf("publish Plan LLM call completed: %w", err)
	}
	return nil
}

func publishPlanLLMError(ctx context.Context, sink event.Sink, callErr error) error {
	if sink == nil || callErr == nil {
		return nil
	}
	return sink.Publish(context.WithoutCancel(ctx), event.ErrorOccurred{Error: event.NewErrorInfo(callErr)})
}

func dependenciesCompleted(graph ExecutionGraph, task Task) bool {
	for _, dependency := range task.Dependencies {
		if graphTask(graph, dependency).Status != TaskStatusCompleted {
			return false
		}
	}
	return true
}

func cloneToolSpecs(specs []tool.Spec) []tool.Spec {
	cloned := make([]tool.Spec, len(specs))
	for index, spec := range specs {
		cloned[index] = spec.Clone()
	}
	return cloned
}
