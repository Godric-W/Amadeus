package tui

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

type CommandHandler func(context.Context, string) error
type TaskHandler func(context.Context, string) error
type TaskContextFactory func(context.Context) (context.Context, context.CancelFunc, error)

const defaultPrompt = "amadeus> "

var slashCommands = []string{
	"/help",
	"/plan",
	"/exit",
	"/clear",
	"/resume",
	"/status",
	"/tools",
}

type TerminalInteractionController struct {
	input        io.Reader
	output       io.Writer
	status       io.Writer
	commands     CommandHandler
	task         TaskHandler
	history      []string
	historyIndex int
	prompt       string
	newTask      TaskContextFactory
	capabilities TerminalCapabilities
}

func NewTerminalInteractionController(input io.Reader, output, status io.Writer, commands CommandHandler, task TaskHandler) (*TerminalInteractionController, error) {
	if input == nil || output == nil || status == nil {
		return nil, errors.New("terminal interaction streams are nil")
	}
	if task == nil {
		return nil, errors.New("terminal interaction task handler is nil")
	}
	return &TerminalInteractionController{
		input: input, output: output, status: status, commands: commands, task: task,
		prompt: defaultPrompt, newTask: defaultTaskContext,
	}, nil
}

func (controller *TerminalInteractionController) WithPrompt(prompt string) *TerminalInteractionController {
	if controller != nil {
		controller.prompt = prompt
	}
	return controller
}

func (controller *TerminalInteractionController) WithTaskContextFactory(factory TaskContextFactory) *TerminalInteractionController {
	if controller != nil && factory != nil {
		controller.newTask = factory
	}
	return controller
}

func (controller *TerminalInteractionController) WithCapabilities(capabilities TerminalCapabilities) *TerminalInteractionController {
	if controller != nil {
		controller.capabilities = capabilities
	}
	return controller
}

func SlashCommands() []string {
	return append([]string(nil), slashCommands...)
}

func CompleteSlashCommand(prefix string) []string {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	if prefix == "" || prefix[0] != '/' {
		return nil
	}
	var matches []string
	for _, command := range slashCommands {
		if strings.HasPrefix(command, prefix) {
			matches = append(matches, command)
		}
	}
	return matches
}

func (controller *TerminalInteractionController) History() []string {
	if controller == nil {
		return nil
	}
	return append([]string(nil), controller.history...)
}

func (controller *TerminalInteractionController) PreviousHistory() string {
	if controller == nil || len(controller.history) == 0 {
		return ""
	}
	if controller.historyIndex <= 0 || controller.historyIndex > len(controller.history) {
		controller.historyIndex = len(controller.history)
	}
	controller.historyIndex--
	return controller.history[controller.historyIndex]
}

func (controller *TerminalInteractionController) NextHistory() string {
	if controller == nil || len(controller.history) == 0 {
		return ""
	}
	if controller.historyIndex < 0 {
		controller.historyIndex = 0
	}
	if controller.historyIndex >= len(controller.history)-1 {
		controller.historyIndex = len(controller.history)
		return ""
	}
	controller.historyIndex++
	return controller.history[controller.historyIndex]
}

func (controller *TerminalInteractionController) Run(ctx context.Context) error {
	if controller == nil {
		return errors.New("terminal interaction controller is nil")
	}
	if ctx == nil {
		return errors.New("terminal interaction context is nil")
	}
	if controller.capabilities.TTY && !controller.capabilities.Plain {
		if file, ok := controller.input.(*os.File); ok {
			return controller.runRaw(ctx, file)
		}
	}
	return controller.runLines(ctx)
}

func (controller *TerminalInteractionController) runLines(ctx context.Context) error {
	reader := bufio.NewReader(controller.input)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := controller.writePrompt(); err != nil {
			return err
		}
		line, readErr := reader.ReadString('\n')
		if len(line) > 0 {
			stop, err := controller.dispatchLine(ctx, trimLineEnding(line))
			if err != nil {
				if writeErr := controller.writeError(err); writeErr != nil {
					return errors.Join(err, writeErr)
				}
			}
			if stop {
				return nil
			}
		}
		if readErr == nil {
			continue
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		return fmt.Errorf("read terminal input: %w", readErr)
	}
}

func (controller *TerminalInteractionController) runRaw(ctx context.Context, input *os.File) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		restore, err := makeTerminalRaw(input)
		if err != nil {
			return controller.runLines(ctx)
		}
		line, action, readErr := controller.readRawLine(ctx, input)
		restoreErr := restore()
		if restoreErr != nil {
			return restoreErr
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
		switch action {
		case rawActionExit:
			return nil
		case rawActionCancel:
			continue
		}
		stop, err := controller.dispatchLine(ctx, line)
		if err != nil {
			if writeErr := controller.writeError(err); writeErr != nil {
				return errors.Join(err, writeErr)
			}
		}
		if stop {
			return nil
		}
	}
}

type rawAction int

const (
	rawActionSubmit rawAction = iota
	rawActionCancel
	rawActionExit
)

func (controller *TerminalInteractionController) readRawLine(ctx context.Context, input io.Reader) (string, rawAction, error) {
	line := ""
	controller.historyIndex = len(controller.history)
	if err := controller.redrawInput(line); err != nil {
		return "", rawActionExit, err
	}
	var buffer [1]byte
	for {
		if err := ctx.Err(); err != nil {
			return "", rawActionExit, err
		}
		count, err := input.Read(buffer[:])
		if err != nil {
			return "", rawActionExit, err
		}
		if count == 0 {
			return "", rawActionExit, io.ErrNoProgress
		}
		switch buffer[0] {
		case '\r', '\n':
			_, err := io.WriteString(controller.status, "\r\x1b[2K"+controller.prompt+line+"\n")
			return line, rawActionSubmit, err
		case 3:
			_, err := io.WriteString(controller.status, "\r\x1b[2K^C\n")
			return "", rawActionCancel, err
		case 4:
			if line == "" {
				_, err := io.WriteString(controller.status, "\r\x1b[2K")
				return "", rawActionExit, err
			}
		case 9:
			line = controller.completeRawInput(line)
		case 27:
			sequence, sequenceErr := readEscapeSequence(input)
			if sequenceErr != nil && !errors.Is(sequenceErr, io.EOF) {
				return "", rawActionExit, sequenceErr
			}
			switch sequence {
			case "[A":
				line = controller.PreviousHistory()
			case "[B":
				line = controller.NextHistory()
			default:
				line = ""
			}
		case 8, 127:
			if len(line) > 0 {
				line = line[:len(line)-1]
			}
		default:
			if buffer[0] >= 32 {
				line += string(buffer[0])
			}
		}
		if err := controller.redrawInput(line); err != nil {
			return "", rawActionExit, err
		}
	}
}

func readEscapeSequence(input io.Reader) (string, error) {
	var first [1]byte
	if _, err := input.Read(first[:]); err != nil {
		return "", err
	}
	if first[0] != '[' {
		return string(first[:]), nil
	}
	var second [1]byte
	if _, err := input.Read(second[:]); err != nil {
		return "", err
	}
	return string([]byte{first[0], second[0]}), nil
}

func (controller *TerminalInteractionController) completeRawInput(line string) string {
	matches := CompleteSlashCommand(line)
	if len(matches) == 1 {
		return matches[0]
	}
	if len(matches) > 1 {
		_, _ = fmt.Fprintf(controller.status, "\r\x1b[2K%s\n", strings.Join(matches, "  "))
	}
	return line
}

func (controller *TerminalInteractionController) redrawInput(line string) error {
	_, err := fmt.Fprintf(controller.status, "\r\x1b[2K%s%s", controller.prompt, line)
	return err
}

func (controller *TerminalInteractionController) dispatchLine(ctx context.Context, line string) (bool, error) {
	line = strings.TrimSpace(line)
	if line == "" || line == "\x03" || line == "\x1b" {
		return false, nil
	}
	if line == "/exit" {
		return true, nil
	}
	controller.history = append(controller.history, line)
	controller.historyIndex = len(controller.history)
	if line == "/plan" {
		return false, errors.New("usage: /plan <task>")
	}
	if strings.HasPrefix(line, "/") && !isPlanTask(line) && controller.commands != nil {
		return false, controller.commands(ctx, line)
	}
	taskCtx, cancel, err := controller.newTask(ctx)
	if err != nil {
		return false, err
	}
	if taskCtx == nil || cancel == nil {
		if cancel != nil {
			cancel()
		}
		return false, errors.New("terminal interaction task context factory returned nil")
	}
	defer cancel()
	return false, controller.task(taskCtx, line)
}

func isPlanTask(value string) bool {
	return strings.HasPrefix(value, "/plan ") || strings.HasPrefix(value, "/plan\t")
}

func (controller *TerminalInteractionController) writePrompt() error {
	if controller.prompt == "" {
		return nil
	}
	if _, err := io.WriteString(controller.status, controller.prompt); err != nil {
		return fmt.Errorf("write terminal prompt: %w", err)
	}
	return nil
}

func (controller *TerminalInteractionController) writeError(err error) error {
	_, writeErr := fmt.Fprintf(controller.status, "error: %s\n", strings.Join(strings.Fields(err.Error()), " "))
	return writeErr
}

func defaultTaskContext(parent context.Context) (context.Context, context.CancelFunc, error) {
	ctx, cancel := context.WithCancel(parent)
	return ctx, cancel, nil
}

func trimLineEnding(line string) string {
	line = strings.TrimSuffix(line, "\n")
	return strings.TrimSuffix(line, "\r")
}
