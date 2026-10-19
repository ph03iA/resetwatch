package quota

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

type claudeSession struct {
	ctx     context.Context
	command *exec.Cmd
	input   io.WriteCloser
	output  *bufio.Scanner
}

type claudeControlResponse struct {
	Type     string `json:"type"`
	Response struct {
		Subtype   string          `json:"subtype"`
		RequestID string          `json:"request_id"`
		Response  json.RawMessage `json:"response"`
		Error     string          `json:"error"`
	} `json:"response"`
}

// startClaude launches Claude Code in the SDK's stream-json mode. Empty setting sources skip user and
// project settings, so no hooks, plugins or MCP servers run; no prompt is ever written, so no model request starts.
func startClaude(ctx context.Context, binary string) (*claudeSession, error) {
	command := exec.CommandContext(ctx, binary, "--output-format", "stream-json", "--verbose",
		"--input-format", "stream-json", "--strict-mcp-config", "--setting-sources", "")
	command.Dir = os.TempDir()
	command.Env = append(os.Environ(), "ENABLE_CLAUDEAI_MCP_SERVERS=false", "CLAUDE_CODE_AUTO_CONNECT_IDE=0",
		"CLAUDE_CODE_IDE_SKIP_AUTO_INSTALL=1")
	input, err := command.StdinPipe()
	if err != nil {
		return nil, errors.New("could not open Claude Code input stream")
	}
	output, err := command.StdoutPipe()
	if err != nil {
		_ = input.Close()
		return nil, errors.New("could not open Claude Code output stream")
	}
	if err := command.Start(); err != nil {
		_ = input.Close()
		_ = output.Close()
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("Claude read stopped: %w", err)
		}
		return nil, errors.New("could not start Claude Code")
	}
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	return &claudeSession{ctx: ctx, command: command, input: input, output: scanner}, nil
}

func (session *claudeSession) close() {
	_ = session.input.Close()
	_ = session.command.Process.Kill()
	_ = session.command.Wait()
}

func (session *claudeSession) request(id string, request map[string]any, result any) error {
	message := map[string]any{"type": "control_request", "request_id": id, "request": request}
	if err := json.NewEncoder(session.input).Encode(message); err != nil {
		return errors.New("could not write to Claude Code")
	}
	for session.output.Scan() {
		var response claudeControlResponse
		if json.Unmarshal(session.output.Bytes(), &response) != nil || response.Type != "control_response" ||
			response.Response.RequestID != id {
			continue // other stream messages, such as system events
		}
		return decodeClaudeResponse(response, request["subtype"], result)
	}
	if err := session.ctx.Err(); err != nil {
		return fmt.Errorf("Claude read stopped: %w", err)
	}
	return errors.New("Claude Code exited before answering; run claude once to finish sign-in or updates")
}

func decodeClaudeResponse(response claudeControlResponse, subtype any, result any) error {
	if response.Response.Subtype != "success" {
		return fmt.Errorf("Claude Code %v request failed: %s", subtype, response.Response.Error)
	}
	if json.Unmarshal(response.Response.Response, result) != nil {
		return fmt.Errorf("Claude Code returned invalid %v data", subtype)
	}
	return nil
}
