package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Godric-W/Amadeus/internal/project"
)

const maxMessageBytes = 16 << 20

type Severity string

const (
	SeverityError       Severity = "error"
	SeverityWarning     Severity = "warning"
	SeverityInformation Severity = "information"
	SeverityHint        Severity = "hint"
)

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Diagnostic struct {
	Range    Range    `json:"range"`
	Severity Severity `json:"severity"`
	Code     string   `json:"code,omitempty"`
	Source   string   `json:"source,omitempty"`
	Message  string   `json:"message"`
}

type Document struct {
	Path    string
	Content string
}

type Client interface {
	Start(context.Context) error
	Observe(context.Context, Document) ([]Diagnostic, error)
	Diagnostics(string) []Diagnostic
	Close(context.Context) error
}

type ProcessOptions struct {
	Command string
	Args    []string
	Root    project.Root
	Timeout time.Duration
}

type ProcessClient struct {
	options ProcessOptions

	mutex       sync.Mutex
	command     *exec.Cmd
	input       io.WriteCloser
	responses   map[string]chan rpcMessage
	diagnostics map[string][]Diagnostic
	opened      map[string]bool
	versions    map[string]int
	nextID      int64
	closed      bool
	readErr     error
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func NewProcessClient(options ProcessOptions) (*ProcessClient, error) {
	if strings.TrimSpace(options.Command) == "" {
		return nil, errors.New("LSP command is empty")
	}
	if options.Root.Path() == "" {
		return nil, errors.New("LSP project root is empty")
	}
	if options.Timeout <= 0 {
		options.Timeout = 10 * time.Second
	}
	return &ProcessClient{
		options: options, responses: make(map[string]chan rpcMessage), diagnostics: make(map[string][]Diagnostic),
		opened: make(map[string]bool), versions: make(map[string]int),
	}, nil
}

func (client *ProcessClient) Start(ctx context.Context) error {
	if client == nil {
		return errors.New("LSP client is nil")
	}
	if ctx == nil {
		return errors.New("LSP start context is nil")
	}
	client.mutex.Lock()
	if client.closed {
		client.mutex.Unlock()
		return errors.New("LSP client is closed")
	}
	if client.command != nil {
		client.mutex.Unlock()
		return nil
	}
	command := exec.Command(client.options.Command, client.options.Args...)
	command.Dir = client.options.Root.Path()
	input, err := command.StdinPipe()
	if err != nil {
		client.mutex.Unlock()
		return fmt.Errorf("create LSP stdin: %w", err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		client.mutex.Unlock()
		return fmt.Errorf("create LSP stdout: %w", err)
	}
	if err := command.Start(); err != nil {
		client.mutex.Unlock()
		return fmt.Errorf("start LSP process: %w", err)
	}
	client.command = command
	client.input = input
	client.mutex.Unlock()
	go client.readLoop(output)

	initializeCtx, cancel := client.withTimeout(ctx)
	defer cancel()
	_, err = client.request(initializeCtx, "initialize", map[string]any{
		"processId": nil, "rootUri": fileURI(client.options.Root.Path()), "capabilities": map[string]any{},
	})
	if err != nil {
		_ = client.Close(context.Background())
		return fmt.Errorf("initialize LSP process: %w", err)
	}
	return nil
}

func (client *ProcessClient) Observe(ctx context.Context, document Document) ([]Diagnostic, error) {
	if ctx == nil {
		return nil, errors.New("LSP observe context is nil")
	}
	if err := client.Start(ctx); err != nil {
		return nil, err
	}
	path, err := client.documentPath(document.Path)
	if err != nil {
		return nil, err
	}
	uri := fileURI(path)
	client.mutex.Lock()
	version := client.versions[uri] + 1
	client.versions[uri] = version
	opened := client.opened[uri]
	client.opened[uri] = true
	client.mutex.Unlock()
	params := map[string]any{"textDocument": map[string]any{"uri": uri, "version": version}, "contentChanges": []map[string]string{{"text": document.Content}}}
	method := "textDocument/didChange"
	if !opened {
		method = "textDocument/didOpen"
		params = map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": languageID(path), "version": version, "text": document.Content}}
	}
	if err := client.notify(ctx, method, params); err != nil {
		return nil, err
	}
	return client.Diagnostics(path), nil
}

func (client *ProcessClient) Diagnostics(path string) []Diagnostic {
	if client == nil {
		return nil
	}
	uri := path
	if absolute, err := client.documentPath(path); err == nil {
		uri = fileURI(absolute)
	}
	client.mutex.Lock()
	values := append([]Diagnostic(nil), client.diagnostics[uri]...)
	client.mutex.Unlock()
	return values
}

func (client *ProcessClient) Close(ctx context.Context) error {
	if client == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("LSP close context is nil")
	}
	client.mutex.Lock()
	if client.closed {
		client.mutex.Unlock()
		return nil
	}
	client.closed = true
	command := client.command
	input := client.input
	client.mutex.Unlock()
	if command == nil {
		return nil
	}
	closeCtx, cancel := client.withTimeout(ctx)
	defer cancel()
	_, shutdownErr := client.request(closeCtx, "shutdown", nil)
	notifyErr := client.notify(closeCtx, "exit", nil)
	if input != nil {
		_ = input.Close()
	}
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	select {
	case err := <-wait:
		if err != nil && shutdownErr == nil && notifyErr == nil {
			return fmt.Errorf("wait for LSP process: %w", err)
		}
	case <-closeCtx.Done():
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		<-wait
		if shutdownErr == nil && notifyErr == nil {
			return closeCtx.Err()
		}
	}
	return errors.Join(shutdownErr, notifyErr)
}

func (client *ProcessClient) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	client.mutex.Lock()
	if client.input == nil || client.closed && method != "shutdown" {
		client.mutex.Unlock()
		return nil, errors.New("LSP process is not running")
	}
	client.nextID++
	id := strconv.FormatInt(client.nextID, 10)
	response := make(chan rpcMessage, 1)
	client.responses[id] = response
	client.mutex.Unlock()
	if err := client.write(rpcMessage{JSONRPC: "2.0", ID: json.RawMessage(id), Method: method, Params: encodeParams(params)}); err != nil {
		client.removeResponse(id)
		return nil, err
	}
	select {
	case message := <-response:
		if message.Error != nil {
			return nil, fmt.Errorf("LSP %s failed (%d): %s", method, message.Error.Code, message.Error.Message)
		}
		return append(json.RawMessage(nil), message.Result...), nil
	case <-ctx.Done():
		client.removeResponse(id)
		return nil, ctx.Err()
	}
}

func (client *ProcessClient) notify(ctx context.Context, method string, params any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return client.write(rpcMessage{JSONRPC: "2.0", Method: method, Params: encodeParams(params)})
}

func (client *ProcessClient) write(message rpcMessage) error {
	encoded, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode LSP message: %w", err)
	}
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if client.input == nil {
		return errors.New("LSP process is not running")
	}
	if _, err := fmt.Fprintf(client.input, "Content-Length: %d\r\n\r\n", len(encoded)); err != nil {
		return fmt.Errorf("write LSP message header: %w", err)
	}
	if _, err := client.input.Write(encoded); err != nil {
		return fmt.Errorf("write LSP message body: %w", err)
	}
	return nil
}

func (client *ProcessClient) readLoop(output io.Reader) {
	reader := bufio.NewReader(output)
	for {
		message, err := readMessage(reader)
		if err != nil {
			client.mutex.Lock()
			if !errors.Is(err, io.EOF) {
				client.readErr = err
			}
			for id, response := range client.responses {
				delete(client.responses, id)
				close(response)
			}
			client.mutex.Unlock()
			return
		}
		client.dispatch(message)
	}
}

func (client *ProcessClient) dispatch(message rpcMessage) {
	if len(message.ID) > 0 {
		id := strings.Trim(string(message.ID), "\"")
		client.mutex.Lock()
		response, exists := client.responses[id]
		if exists {
			delete(client.responses, id)
		}
		client.mutex.Unlock()
		if exists {
			response <- message
			close(response)
		}
		return
	}
	if message.Method != "textDocument/publishDiagnostics" {
		return
	}
	var params struct {
		URI         string          `json:"uri"`
		Diagnostics []rawDiagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(message.Params, &params); err != nil || params.URI == "" {
		return
	}
	diagnostics := make([]Diagnostic, 0, len(params.Diagnostics))
	for _, diagnostic := range params.Diagnostics {
		diagnostics = append(diagnostics, diagnostic.normalized())
	}
	client.mutex.Lock()
	client.diagnostics[params.URI] = diagnostics
	client.mutex.Unlock()
}

type rawDiagnostic struct {
	Range struct {
		Start Position `json:"start"`
		End   Position `json:"end"`
	} `json:"range"`
	Severity int             `json:"severity"`
	Code     json.RawMessage `json:"code"`
	Source   string          `json:"source"`
	Message  string          `json:"message"`
}

func (diagnostic rawDiagnostic) normalized() Diagnostic {
	severity := SeverityInformation
	switch diagnostic.Severity {
	case 1:
		severity = SeverityError
	case 2:
		severity = SeverityWarning
	case 4:
		severity = SeverityHint
	}
	return Diagnostic{Range: Range{Start: diagnostic.Range.Start, End: diagnostic.Range.End}, Severity: severity, Code: strings.Trim(string(diagnostic.Code), "\""), Source: diagnostic.Source, Message: strings.TrimSpace(diagnostic.Message)}
}

func readMessage(reader *bufio.Reader) (rpcMessage, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return rpcMessage{}, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 && strings.EqualFold(strings.TrimSpace(parts[0]), "Content-Length") {
			value, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil || value < 1 || value > maxMessageBytes {
				return rpcMessage{}, errors.New("invalid LSP content length")
			}
			contentLength = value
		}
	}
	if contentLength < 0 {
		return rpcMessage{}, errors.New("LSP message has no content length")
	}
	content := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, content); err != nil {
		return rpcMessage{}, err
	}
	var message rpcMessage
	if err := json.Unmarshal(content, &message); err != nil {
		return rpcMessage{}, fmt.Errorf("decode LSP message: %w", err)
	}
	return message, nil
}

func (client *ProcessClient) documentPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("LSP document path is empty")
	}
	if filepath.IsAbs(path) {
		relative, err := client.options.Root.Relative(path)
		if err != nil {
			return "", fmt.Errorf("LSP document is outside project root: %w", err)
		}
		path = relative
	}
	resolved, err := client.options.Root.Resolve(path)
	if err != nil {
		return "", fmt.Errorf("resolve LSP document: %w", err)
	}
	return resolved, nil
}

func (client *ProcessClient) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, client.options.Timeout)
}

func (client *ProcessClient) removeResponse(id string) {
	client.mutex.Lock()
	delete(client.responses, id)
	client.mutex.Unlock()
}

func encodeParams(value any) json.RawMessage {
	if value == nil {
		return nil
	}
	encoded, _ := json.Marshal(value)
	return encoded
}

func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}

func languageID(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	default:
		return "plaintext"
	}
}

func diagnosticsJSON(values []Diagnostic) []byte {
	encoded, _ := json.Marshal(values)
	return bytes.TrimSpace(encoded)
}

var _ Client = (*ProcessClient)(nil)
