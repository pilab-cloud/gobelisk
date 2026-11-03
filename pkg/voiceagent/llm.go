package voiceagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/devopshaven/gobelisk/pkg/config"
)

// Message is one chat message in the OpenAI-compatible wire format.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall is a function invocation requested by the model.
type ToolCall struct {
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function"`
}

// FunctionCall carries the tool name and JSON arguments.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool is a function the model may call. Arguments are passed as raw JSON.
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
	Execute     func(ctx context.Context, arguments json.RawMessage) (string, error)
}

// LLM produces the assistant's next reply.
type LLM interface {
	Chat(ctx context.Context, system string, history []Message) (string, error)
}

// OpenAILLM talks to any OpenAI-compatible chat completions endpoint. It
// implements the function-calling loop so the agent can use MCP-style tools.
type OpenAILLM struct {
	endpoint    string
	apiKey      string
	model       string
	temperature float64
	maxTokens   int
	keepAlive   string
	tools       []Tool
	client      *http.Client
}

// NewOpenAILLM builds an OpenAI-compatible client. The same client works with
// Ollama (/v1/chat/completions), vLLM and llama.cpp server.
func NewOpenAILLM(cfg config.VoiceAgentLLMConfig) *OpenAILLM {
	return &OpenAILLM{
		endpoint:    cfg.Endpoint,
		apiKey:      cfg.APIKey,
		model:       cfg.Model,
		temperature: cfg.Temperature,
		maxTokens:   cfg.MaxTokens,
		keepAlive:   cfg.KeepAlive,
		client:      &http.Client{Timeout: cfg.Timeout()},
	}
}

// WithTools registers tools that the model may call.
func (l *OpenAILLM) WithTools(tools ...Tool) *OpenAILLM {
	l.tools = append(l.tools, tools...)
	return l
}

// Chat implements LLM.
func (l *OpenAILLM) Chat(ctx context.Context, system string, history []Message) (string, error) {
	messages := make([]Message, 0, len(history)+1)
	if strings.TrimSpace(system) != "" {
		messages = append(messages, Message{Role: "system", Content: system})
	}
	messages = append(messages, history...)

	// A small bounded loop is enough for tool use in a live conversation.
	for round := 0; round < 4; round++ {
		reply, err := l.complete(ctx, messages)
		if err != nil {
			return "", err
		}
		if len(reply.ToolCalls) == 0 {
			return strings.TrimSpace(reply.Content), nil
		}

		messages = append(messages, *reply)
		for _, call := range reply.ToolCalls {
			result, err := l.runTool(ctx, call)
			if err != nil {
				result = fmt.Sprintf("tool error: %v", err)
			}
			messages = append(messages, Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: call.ID,
			})
		}
	}
	return "", fmt.Errorf("llm: tool loop exceeded")
}

func (l *OpenAILLM) complete(ctx context.Context, messages []Message) (*Message, error) {
	payload := map[string]any{
		"model":       l.model,
		"messages":    messages,
		"temperature": l.temperature,
		"max_tokens":  l.maxTokens,
		"stream":      false,
	}
	if l.keepAlive != "" {
		payload["keep_alive"] = l.keepAlive
	}
	if len(l.tools) > 0 {
		tools := make([]map[string]any, 0, len(l.tools))
		for _, tool := range l.tools {
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        tool.Name,
					"description": tool.Description,
					"parameters":  tool.Parameters,
				},
			})
		}
		payload["tools"] = tools
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if l.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+l.apiKey)
	}

	resp, err := l.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llm request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("llm read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("llm: %d %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var out struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("llm decode: %w", err)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("llm: empty choices")
	}
	return &out.Choices[0].Message, nil
}

func (l *OpenAILLM) runTool(ctx context.Context, call ToolCall) (string, error) {
	for _, tool := range l.tools {
		if tool.Name != call.Function.Name {
			continue
		}
		if tool.Execute == nil {
			return "", fmt.Errorf("tool %q has no executor", tool.Name)
		}
		args := []byte(call.Function.Arguments)
		if len(args) == 0 || !json.Valid(args) {
			args = []byte("{}")
		}
		return tool.Execute(ctx, args)
	}
	return "", fmt.Errorf("unknown tool %q", call.Function.Name)
}
