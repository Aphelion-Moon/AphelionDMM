package meridian

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultMCPTimeout = 30 * time.Second
	defaultMaxBytes   = int64(1 << 20)
	mcpProtocol       = "2025-06-18"
)

var requiredTools = []string{"dm_check_errors", "dm_map_info", "dm_parse_environment"}

type mcpClient struct {
	config           Config
	repositories     map[string]Repository
	command          *exec.Cmd
	stdin            io.WriteCloser
	responses        chan responseLine
	done             chan error
	mu               sync.Mutex
	nextID           uint64
	serverVersion    string
	parsedRepository string
	stateGeneration  uint64
	closed           bool
}

type responseLine struct {
	data []byte
	err  error
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code int `json:"code"`
}

type initializeResult struct {
	ProtocolVersion string `json:"protocolVersion"`
	ServerInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
}

type toolsResult struct {
	Tools []struct {
		Name string `json:"name"`
	} `json:"tools"`
}

type toolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

// NewMCP starts and negotiates a bounded Meridian-MCP stdio client.
func NewMCP(ctx context.Context, config Config) (Client, error) {
	normalized, repositories, err := normalizeConfig(config)
	if err != nil {
		return nil, err
	}
	command := exec.Command(normalized.Executable, normalized.Arguments...)
	command.Env = append([]string(nil), os.Environ()...)
	for key, value := range normalized.Environment {
		if strings.ContainsAny(key, "=\x00") || strings.ContainsRune(value, '\x00') {
			return nil, fmt.Errorf("Meridian-MCP environment contains an invalid value")
		}
		command.Env = append(command.Env, key+"="+value)
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("prepare Meridian-MCP stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("prepare Meridian-MCP stdout: %w", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("prepare Meridian-MCP stderr: %w", err)
	}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start Meridian-MCP: %w", err)
	}

	client := &mcpClient{
		config:       normalized,
		repositories: repositories,
		command:      command,
		stdin:        stdin,
		responses:    make(chan responseLine, 1),
		done:         make(chan error, 1),
	}
	go client.readResponses(stdout)
	go func() {
		_, _ = io.Copy(io.Discard, stderr)
	}()
	go func() {
		client.done <- command.Wait()
	}()

	if err := client.initialize(ctx); err != nil {
		client.terminate()
		return nil, err
	}
	return client, nil
}

func (client *mcpClient) ParseEnvironment(ctx context.Context, repositoryID, dmeID string) (EnvironmentResult, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	repository, ok := client.repositories[repositoryID]
	if !ok || repository.Identity != repositoryID {
		return EnvironmentResult{}, fmt.Errorf("unknown repository identity")
	}
	if dmeID != repository.DME {
		return EnvironmentResult{}, fmt.Errorf("unknown DME identifier")
	}
	path, err := resolveContained(repository.Root, repository.DMEPath)
	if err != nil {
		return EnvironmentResult{}, err
	}
	payload, err := client.callToolLocked(ctx, "dm_parse_environment", map[string]string{"dme_path": path})
	if err != nil {
		return EnvironmentResult{}, err
	}
	var decoded struct {
		StateGeneration uint64 `json:"state_generation"`
		TotalTypes      uint64 `json:"total_types"`
		IndexedSymbols  uint64 `json:"indexed_symbols"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return EnvironmentResult{}, fmt.Errorf("Meridian-MCP parse response is invalid")
	}
	if decoded.StateGeneration == 0 {
		return EnvironmentResult{}, fmt.Errorf("Meridian-MCP parse response has no state generation")
	}
	client.parsedRepository = repositoryID
	client.stateGeneration = decoded.StateGeneration
	return EnvironmentResult{
		RepositoryID: repositoryID, DMEIdentifier: dmeID, StateGeneration: decoded.StateGeneration,
		MCPVersion: client.serverVersion, TotalTypes: decoded.TotalTypes, IndexedSymbols: decoded.IndexedSymbols, Raw: payload,
	}, nil
}

func (client *mcpClient) InspectMap(ctx context.Context, repositoryID, mapTargetID string) (MapResult, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	repository, err := client.requireParsedRepository(repositoryID)
	if err != nil {
		return MapResult{}, err
	}
	relative, ok := repository.Targets[mapTargetID]
	if !ok {
		return MapResult{}, fmt.Errorf("unknown map target identifier")
	}
	path, err := resolveContained(repository.Root, relative)
	if err != nil {
		return MapResult{}, err
	}
	payload, err := client.callToolLocked(ctx, "dm_map_info", map[string]string{"dmm_path": path})
	if err != nil {
		return MapResult{}, err
	}
	var decoded struct {
		Width      uint64 `json:"width"`
		Height     uint64 `json:"height"`
		Levels     uint64 `json:"z_levels"`
		Dimensions struct {
			X uint64 `json:"x"`
			Y uint64 `json:"y"`
			Z uint64 `json:"z"`
		} `json:"dimensions"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return MapResult{}, fmt.Errorf("Meridian-MCP map response is invalid")
	}
	if decoded.Width == 0 {
		decoded.Width, decoded.Height, decoded.Levels = decoded.Dimensions.X, decoded.Dimensions.Y, decoded.Dimensions.Z
	}
	return MapResult{
		RepositoryID: repositoryID, MapTargetID: mapTargetID, StateGeneration: client.stateGeneration,
		MCPVersion: client.serverVersion, Width: decoded.Width, Height: decoded.Height, Levels: decoded.Levels, Raw: payload,
	}, nil
}

func (client *mcpClient) CheckErrors(ctx context.Context, repositoryID string) (DiagnosticResult, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if _, err := client.requireParsedRepository(repositoryID); err != nil {
		return DiagnosticResult{}, err
	}
	payload, err := client.callToolLocked(ctx, "dm_check_errors", map[string]string{})
	if err != nil {
		return DiagnosticResult{}, err
	}
	var decoded struct {
		Count       *uint64         `json:"count"`
		TotalCount  *uint64         `json:"total_count"`
		Truncated   bool            `json:"truncated"`
		Diagnostics json.RawMessage `json:"diagnostics"`
		Pagination  struct {
			HasMore bool `json:"has_more"`
		} `json:"pagination"`
		Summary struct {
			Total      *uint64           `json:"total"`
			BySeverity map[string]uint64 `json:"by_severity"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return DiagnosticResult{}, fmt.Errorf("Meridian-MCP diagnostic response is invalid")
	}
	var entries []json.RawMessage
	if decoded.Count == nil || json.Unmarshal(decoded.Diagnostics, &entries) != nil || *decoded.Count != uint64(len(entries)) {
		return DiagnosticResult{}, fmt.Errorf("Meridian-MCP diagnostic page count is invalid")
	}
	count := *decoded.Count
	truncated := decoded.Truncated || decoded.Pagination.HasMore
	if decoded.TotalCount != nil {
		if *decoded.TotalCount < count {
			return DiagnosticResult{}, fmt.Errorf("Meridian-MCP diagnostic total is smaller than its page")
		}
		count = *decoded.TotalCount
		truncated = truncated || count > *decoded.Count
	} else if truncated {
		return DiagnosticResult{}, fmt.Errorf("Meridian-MCP truncated diagnostics omit their total")
	}
	if decoded.Summary.Total != nil && *decoded.Summary.Total != count {
		return DiagnosticResult{}, fmt.Errorf("Meridian-MCP diagnostic summary total is inconsistent")
	}
	if decoded.Summary.BySeverity != nil {
		var sum uint64
		for _, severityCount := range decoded.Summary.BySeverity {
			if severityCount > count-sum {
				return DiagnosticResult{}, fmt.Errorf("Meridian-MCP diagnostic severity totals are inconsistent")
			}
			sum += severityCount
		}
		if sum != count {
			return DiagnosticResult{}, fmt.Errorf("Meridian-MCP diagnostic severity totals are incomplete")
		}
	}
	return DiagnosticResult{
		RepositoryID: repositoryID, StateGeneration: client.stateGeneration, MCPVersion: client.serverVersion,
		Count: count, ReturnedCount: *decoded.Count, Truncated: truncated, SeverityCounts: decoded.Summary.BySeverity,
		Diagnostics: decoded.Diagnostics, Raw: payload,
	}, nil
}

func (client *mcpClient) Close(ctx context.Context) error {
	client.mu.Lock()
	if client.closed {
		client.mu.Unlock()
		return nil
	}
	client.closed = true
	_ = client.stdin.Close()
	client.mu.Unlock()
	select {
	case <-client.done:
		return nil
	case <-ctx.Done():
		client.terminate()
		return fmt.Errorf("close Meridian-MCP: %w", ctx.Err())
	}
}

func (client *mcpClient) initialize(ctx context.Context) error {
	client.mu.Lock()
	defer client.mu.Unlock()
	result, err := client.callLocked(ctx, "initialize", map[string]any{
		"protocolVersion": mcpProtocol,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "apheliondmm", "version": "1"},
	})
	if err != nil {
		return err
	}
	var initialized initializeResult
	if err := json.Unmarshal(result, &initialized); err != nil || initialized.ServerInfo.Name == "" || initialized.ServerInfo.Version == "" {
		return fmt.Errorf("Meridian-MCP initialize response is invalid")
	}
	if initialized.ProtocolVersion != mcpProtocol {
		return fmt.Errorf("Meridian-MCP protocol version is incompatible")
	}
	client.serverVersion = initialized.ServerInfo.Version
	if err := client.notifyLocked("notifications/initialized", map[string]any{}); err != nil {
		return err
	}
	listed, err := client.callLocked(ctx, "tools/list", map[string]any{})
	if err != nil {
		return err
	}
	var tools toolsResult
	if err := json.Unmarshal(listed, &tools); err != nil {
		return fmt.Errorf("Meridian-MCP capability response is invalid")
	}
	available := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		available = append(available, tool.Name)
	}
	sort.Strings(available)
	for _, required := range requiredTools {
		if !containsString(available, required) {
			return fmt.Errorf("Meridian-MCP does not advertise required capability %q", required)
		}
	}
	return nil
}

func (client *mcpClient) callToolLocked(ctx context.Context, name string, arguments any) (json.RawMessage, error) {
	if !containsString(requiredTools, name) {
		return nil, fmt.Errorf("Meridian-MCP capability is not allowed")
	}
	result, err := client.callLocked(ctx, "tools/call", map[string]any{"name": name, "arguments": arguments})
	if err != nil {
		return nil, err
	}
	var tool toolResult
	if err := json.Unmarshal(result, &tool); err != nil || len(tool.Content) != 1 || tool.Content[0].Type != "text" {
		return nil, fmt.Errorf("Meridian-MCP tool response is invalid")
	}
	if tool.IsError {
		return nil, fmt.Errorf("Meridian-MCP tool rejected the request")
	}
	payload := json.RawMessage(tool.Content[0].Text)
	if !json.Valid(payload) || int64(len(payload)) > client.config.MaxBytes {
		return nil, fmt.Errorf("Meridian-MCP tool payload is invalid")
	}
	return payload, nil
}

func (client *mcpClient) callLocked(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if client.closed {
		return nil, fmt.Errorf("Meridian-MCP client is closed")
	}
	client.nextID++
	id := client.nextID
	request, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return nil, fmt.Errorf("encode Meridian-MCP request: %w", err)
	}
	request = append(request, '\n')
	if _, err := client.stdin.Write(request); err != nil {
		return nil, fmt.Errorf("write Meridian-MCP request")
	}
	callCtx, cancel := context.WithTimeout(ctx, client.config.Timeout)
	defer cancel()
	for {
		select {
		case line := <-client.responses:
			if line.err != nil {
				if errors.Is(line.err, bufio.ErrTooLong) || strings.Contains(line.err.Error(), "token too long") {
					return nil, fmt.Errorf("Meridian-MCP response limit exceeded")
				}
				return nil, fmt.Errorf("read Meridian-MCP response")
			}
			var response rpcResponse
			if err := json.Unmarshal(line.data, &response); err != nil || response.JSONRPC != "2.0" {
				return nil, fmt.Errorf("Meridian-MCP response is malformed")
			}
			if response.ID != id {
				return nil, fmt.Errorf("Meridian-MCP response sequence is invalid")
			}
			if response.Error != nil {
				return nil, fmt.Errorf("Meridian-MCP remote error (code %d)", response.Error.Code)
			}
			if len(response.Result) == 0 {
				return nil, fmt.Errorf("Meridian-MCP response has no result")
			}
			return response.Result, nil
		case <-client.done:
			return nil, fmt.Errorf("Meridian-MCP process exited")
		case <-callCtx.Done():
			client.terminate()
			return nil, fmt.Errorf("Meridian-MCP request timed out")
		}
	}
}

func (client *mcpClient) notifyLocked(method string, params any) error {
	request, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		return fmt.Errorf("encode Meridian-MCP notification: %w", err)
	}
	request = append(request, '\n')
	if _, err := client.stdin.Write(request); err != nil {
		return fmt.Errorf("write Meridian-MCP notification")
	}
	return nil
}

func (client *mcpClient) readResponses(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	limit := int(client.config.MaxBytes)
	if limit < 1 {
		limit = int(defaultMaxBytes)
	}
	scanner.Buffer(make([]byte, min(limit, 64<<10)), limit)
	for scanner.Scan() {
		client.responses <- responseLine{data: append([]byte(nil), scanner.Bytes()...)}
	}
	if err := scanner.Err(); err != nil {
		client.responses <- responseLine{err: err}
	}
}

func (client *mcpClient) requireParsedRepository(repositoryID string) (Repository, error) {
	repository, ok := client.repositories[repositoryID]
	if !ok || repository.Identity != repositoryID {
		return Repository{}, fmt.Errorf("unknown repository identity")
	}
	if client.parsedRepository != repositoryID || client.stateGeneration == 0 {
		return Repository{}, fmt.Errorf("dm_parse_environment is required before inspection")
	}
	return repository, nil
}

func (client *mcpClient) terminate() {
	if client.command == nil || client.command.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		killer := exec.Command("taskkill.exe", "/PID", fmt.Sprint(client.command.Process.Pid), "/T", "/F")
		killer.Stdout = io.Discard
		killer.Stderr = io.Discard
		_ = killer.Run()
	}
	_ = client.command.Process.Kill()
}

func normalizeConfig(config Config) (Config, map[string]Repository, error) {
	if config.Executable == "" {
		return Config{}, nil, fmt.Errorf("Meridian-MCP executable is required")
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultMCPTimeout
	}
	if config.MaxBytes <= 0 {
		config.MaxBytes = defaultMaxBytes
	}
	if config.MaxBytes > int64(^uint(0)>>1) {
		return Config{}, nil, fmt.Errorf("Meridian-MCP response limit is too large")
	}
	if len(config.Roots) == 0 {
		return Config{}, nil, fmt.Errorf("at least one Meridian-MCP repository is required")
	}
	repositories := make(map[string]Repository, len(config.Roots))
	for id, repository := range config.Roots {
		if id == "" || repository.Identity != id || repository.Root == "" || repository.DME == "" {
			return Config{}, nil, fmt.Errorf("Meridian-MCP repository configuration is invalid")
		}
		root, err := filepath.Abs(repository.Root)
		if err != nil {
			return Config{}, nil, fmt.Errorf("resolve Meridian-MCP repository root: %w", err)
		}
		root, err = filepath.EvalSymlinks(root)
		if err != nil {
			return Config{}, nil, fmt.Errorf("resolve Meridian-MCP repository root: %w", err)
		}
		repository.Root = filepath.Clean(root)
		repository, err = normalizeRepository(repository)
		if err != nil {
			return Config{}, nil, err
		}
		repositories[id] = repository
	}
	config.Arguments = append([]string(nil), config.Arguments...)
	config.Roots = nil
	return config, repositories, nil
}

func resolveContained(root, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || filepath.VolumeName(relative) != "" {
		return "", fmt.Errorf("configured Meridian-MCP path is invalid")
	}
	path := filepath.Clean(filepath.Join(root, filepath.FromSlash(relative)))
	relation, err := filepath.Rel(root, path)
	if err != nil || relation == ".." || strings.HasPrefix(relation, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("configured Meridian-MCP path escapes repository root")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve configured Meridian-MCP path: %w", err)
	}
	relation, err = filepath.Rel(root, resolved)
	if err != nil || relation == ".." || strings.HasPrefix(relation, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("configured Meridian-MCP path escapes repository root")
	}
	return resolved, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
