package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
)

// ==========================================
// 1. JSON-RPC 2.0 / MCP Protocol Structures
// ==========================================

type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   interface{}     `json:"error,omitempty"`
}

type MCPToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

type MCPListToolsResult struct {
	Tools []MCPToolDefinition `json:"tools"`
}

type MCPCallToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type MCPCallToolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError,omitempty"`
}

// ==========================================
// 2. Embedded MCP Client (Stdio Pipe Transport)
// ==========================================

type MCPClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	msgID  int
}

func StartMCPServer(command string, args ...string) (*MCPClient, error) {
	cmd := exec.Command(command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	client := &MCPClient{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdoutPipe),
		msgID:  1,
	}

	// MCP Initialize handshake
	initReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      client.nextID(),
		Method:  "initialize",
		Params: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]string{
				"name":    "gobelisk-pbx-agent",
				"version": "1.0.0",
			},
		},
	}
	if _, err := client.send(initReq); err != nil {
		return nil, fmt.Errorf("MCP handshake error: %v", err)
	}

	return client, nil
}

func (c *MCPClient) nextID() int {
	c.msgID++
	return c.msgID
}

func (c *MCPClient) send(req JSONRPCRequest) (*JSONRPCResponse, error) {
	data, _ := json.Marshal(req)
	data = append(data, '\n')
	if _, err := c.stdin.Write(data); err != nil {
		return nil, err
	}

	line, err := c.stdout.ReadBytes('\n')
	if err != nil {
		return nil, err
	}

	var resp JSONRPCResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListTools queries the MCP server for all available tools
func (c *MCPClient) ListTools() ([]MCPToolDefinition, error) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.nextID(),
		Method:  "tools/list",
	}
	resp, err := c.send(req)
	if err != nil {
		return nil, err
	}

	var res MCPListToolsResult
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		return nil, err
	}
	return res.Tools, nil
}

// CallTool invokes a specific function on the remote MCP server
func (c *MCPClient) CallTool(name string, args map[string]interface{}) (string, error) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      c.nextID(),
		Method:  "tools/call",
		Params: MCPCallToolParams{
			Name:      name,
			Arguments: args,
		},
	}
	resp, err := c.send(req)
	if err != nil {
		return "", err
	}

	var res MCPCallToolResult
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		return "", err
	}

	if len(res.Content) > 0 {
		return res.Content[0].Text, nil
	}
	return string(resp.Result), nil
}

// ==========================================
// 3. Ollama / LLM Bridge
// ==========================================

type OllamaTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		Parameters  map[string]interface{} `json:"parameters"`
	} `json:"function"`
}

// MCPToOllamaTools transforms an MCP InputSchema into an Ollama/OpenAI tool definition
func MCPToOllamaTools(mcpTools []MCPToolDefinition) []OllamaTool {
	var oTools []OllamaTool
	for _, t := range mcpTools {
		ot := OllamaTool{Type: "function"}
		ot.Function.Name = t.Name
		ot.Function.Description = t.Description
		ot.Function.Parameters = t.InputSchema
		oTools = append(oTools, ot)
	}
	return oTools
}

type ChatMessage struct {
	Role       string      `json:"role"`
	Content    string      `json:"content,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	ToolCalls  []MCPOllama `json:"tool_calls,omitempty"`
}

type MCPOllama struct {
	Function struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	} `json:"function"`
}

func main() {
	fmt.Println("🚀 [MCP + Ollama Pipeline]")
	fmt.Println("1. Discovering MCP tools via tools/list...")
	fmt.Println("2. Converting schemas to Ollama tools payload...")
	fmt.Println("3. Invoking LLM with Hungarian persona system prompt...")
	fmt.Println("4. Dispatching tool call to MCP server via tools/call...")
	fmt.Println("5. Feeding result back to synthesize final Hungarian spoken response.")
}
