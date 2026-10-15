package quota

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
)

type codexClient struct {
	ctx     context.Context
	command *exec.Cmd
	input   io.WriteCloser
	output  *bufio.Scanner
}

type rpcMessage struct {
	ID     *int   `json:"id,omitempty"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code int `json:"code"`
	} `json:"error"`
}

func startCodex(ctx context.Context, binary string) (*codexClient, error) {
	command := exec.CommandContext(ctx, binary, "app-server")
	input, err := command.StdinPipe()
	if err != nil {
		return nil, errors.New("could not open Codex input stream")
	}
	output, err := command.StdoutPipe()
	if err != nil {
		_ = input.Close()
		return nil, errors.New("could not open Codex output stream")
	}
	if err := command.Start(); err != nil {
		_ = input.Close()
		_ = output.Close()
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("Codex read stopped: %w", err)
		}
		return nil, errors.New("could not start Codex app-server; check --codex-bin")
	}
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	return &codexClient{ctx: ctx, command: command, input: input, output: scanner}, nil
}

func (client *codexClient) close() {
	_ = client.input.Close()
	_ = client.command.Process.Kill()
	_ = client.command.Wait()
}

func (client *codexClient) send(message any) error {
	if err := json.NewEncoder(client.input).Encode(message); err != nil {
		return errors.New("could not write to Codex app-server")
	}
	return nil
}

func (client *codexClient) call(id int, method string, params any, result any) error {
	if err := client.send(rpcMessage{ID: &id, Method: method, Params: params}); err != nil {
		return err
	}
	response, err := client.response(id)
	if err != nil {
		return err
	}
	if response.Error != nil {
		return fmt.Errorf("Codex %s failed (RPC code %d)", method, response.Error.Code)
	}
	if len(response.Result) == 0 || string(response.Result) == "null" {
		return errors.New("Codex returned no result")
	}
	if err := json.Unmarshal(response.Result, result); err != nil {
		return errors.New("Codex returned invalid result data")
	}
	return nil
}

func (client *codexClient) response(id int) (rpcResponse, error) {
	for client.output.Scan() {
		var response rpcResponse
		if err := json.Unmarshal(client.output.Bytes(), &response); err != nil {
			return rpcResponse{}, errors.New("Codex returned malformed protocol data")
		}
		if response.Method != "" {
			if err := client.rejectRequest(response); err != nil {
				return rpcResponse{}, err
			}
			continue
		}
		if matchesResponse(response.ID, id) {
			return response, nil
		}
	}
	if err := client.ctx.Err(); err != nil {
		return rpcResponse{}, fmt.Errorf("Codex read stopped: %w", err)
	}
	if client.output.Err() != nil {
		return rpcResponse{}, errors.New("Codex returned unreadable protocol data")
	}
	return rpcResponse{}, errors.New("Codex output ended before the requested result")
}

func matchesResponse(raw json.RawMessage, id int) bool {
	if string(raw) == "null" {
		return false
	}
	var responseID int
	return json.Unmarshal(raw, &responseID) == nil && responseID == id
}

func (client *codexClient) rejectRequest(response rpcResponse) error {
	if len(response.ID) == 0 || string(response.ID) == "null" {
		return nil
	}
	return client.send(map[string]any{
		"id":    response.ID,
		"error": map[string]any{"code": -32601, "message": "Unsupported client request"},
	})
}
