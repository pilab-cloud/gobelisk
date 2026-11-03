package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

// --- 1. OpenAI / Ollama API Structures ---

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolDefinition struct {
	Type     string         `json:"type"`
	Function FunctionSchema `json:"function"`
}

type FunctionSchema struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type ChatRequest struct {
	Model    string           `json:"model"`
	Messages []Message        `json:"messages"`
	Tools    []ToolDefinition `json:"tools,omitempty"`
	Stream   bool             `json:"stream"`
}

type ChatResponse struct {
	Message Message `json:"message"`
}

// --- 2. Local Business Logic / MCP Tool Handlers ---

func executeTool(name string, rawArgs string) (string, error) {
	fmt.Printf("\n⚙️  [MCP Runtime] Executing tool: %s(args: %s)\n", name, rawArgs)

	switch name {
	case "create_freight_order":
		var args struct {
			CargoDescription string `json:"cargo_description"`
			PickupLocation   string `json:"pickup_location"`
			DeliveryLocation string `json:"delivery_location"`
			VehicleType      string `json:"vehicle_type"`
		}
		if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
			return "", err
		}
		// Real dispatch to database / logistics API
		result := map[string]interface{}{
			"status":   "success",
			"order_id": "FO-2026-901",
			"message":  fmt.Sprintf("Freight recorded: %s -> %s (%s, %s)", args.PickupLocation, args.DeliveryLocation, args.CargoDescription, args.VehicleType),
		}
		resBytes, _ := json.Marshal(result)
		return string(resBytes), nil

	case "get_billing_invoice":
		var args struct {
			ServiceType string `json:"service_type"`
			Period      string `json:"period"`
		}
		if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
			return "", err
		}
		// Query billing system
		result := map[string]interface{}{
			"status":         "found",
			"invoice_number": "INV-2026-8812",
			"amount_huf":     8990,
			"due_date":       "2026-11-15",
			"is_paid":        false,
		}
		resBytes, _ := json.Marshal(result)
		return string(resBytes), nil

	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

// --- 3. Chat API Helper ---

func callOllama(req ChatRequest) (*ChatResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	resp, err := http.Post("http://127.0.0.1:11434/api/chat", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(respBytes, &chatResp); err != nil {
		return nil, fmt.Errorf("invalid response JSON: %v, raw: %s", err, string(respBytes))
	}
	return &chatResp, nil
}

// --- 4. Main Agent Loop (ReAct / Tool Execution Loop) ---

func main() {
	modelName := "qwen2.5-coder:7b"

	// List of available English MCP tool definitions
	tools := []ToolDefinition{
		{
			Type: "function",
			Function: FunctionSchema{
				Name:        "create_freight_order",
				Description: "Create a freight delivery request with origin, destination, cargo type and vehicle type.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"cargo_description": map[string]string{"type": "string", "description": "Description of the cargo"},
						"pickup_location":   map[string]string{"type": "string", "description": "Origin city"},
						"delivery_location": map[string]string{"type": "string", "description": "Destination city"},
						"vehicle_type":      map[string]string{"type": "string", "description": "Vehicle type e.g. truck, van"},
					},
					"required": []string{"cargo_description", "pickup_location", "delivery_location"},
				},
			},
		},
		{
			Type: "function",
			Function: FunctionSchema{
				Name:        "get_billing_invoice",
				Description: "Retrieve customer invoice details including amount and due date.",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"service_type": map[string]interface{}{"type": "string", "enum": []string{"internet", "mobile", "tv"}},
						"period":       map[string]interface{}{"type": "string", "enum": []string{"latest", "previous"}},
					},
					"required": []string{"service_type", "period"},
				},
			},
		},
	}

	// Initialize conversation history
	messages := []Message{
		{
			Role:    "system",
			Content: "Te egy magyar ügyfélszolgálat vagy. Mindig használd az elérhető funkciókat, ha a felhasználó fuvarozást kér.",
		},
		{
			Role:    "user",
			Content: "Jó napot kívánok! Csiszolt gerendát szeretnék Budapestről Zalaegerszegre szállítani kamionnal.",
		},
	}

	fmt.Printf("👤 [Customer (Spoken HU)]: %s\n", messages[1].Content)

	// Step 1: Query model with tools
	req := ChatRequest{
		Model:    modelName,
		Messages: messages,
		Tools:    tools,
		Stream:   false,
	}

	resp, err := callOllama(req)
	if err != nil {
		log.Fatalf("Error invoking Ollama: %v", err)
	}

	// Step 2: Check if model requests a tool call
	assistantMsg := resp.Message
	messages = append(messages, assistantMsg) // Append model response to message history

	var toolName string
	var toolArgs string

	if len(assistantMsg.ToolCalls) > 0 {
		toolName = assistantMsg.ToolCalls[0].Function.Name
		toolArgs = assistantMsg.ToolCalls[0].Function.Arguments
	} else if assistantMsg.Content != "" {
		// Fallback parser if tool call is rendered directly in content
		var fallback struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		if err := json.Unmarshal([]byte(assistantMsg.Content), &fallback); err == nil && fallback.Name != "" {
			toolName = fallback.Name
			argBytes, _ := json.Marshal(fallback.Arguments)
			toolArgs = string(argBytes)
		}
	}

	if toolName != "" {
		// Step 3: Execute tool locally / dispatch to MCP server
		toolOutput, err := executeTool(toolName, toolArgs)
		if err != nil {
			log.Fatalf("Tool execution error: %v", err)
		}

		// Step 4: Feed tool execution result back with role: "tool"
		fmt.Printf("📥 [Tool Output Feed]: %s\n", toolOutput)
		messages = append(messages, Message{
			Role:       "tool",
			Content:    toolOutput,
			ToolCallID: "call_1",
		})

		// Step 5: Query model with updated history to synthesize natural spoken Hungarian response
		req2 := ChatRequest{
			Model:    modelName,
			Messages: messages,
			Stream:   false,
		}

		finalResp, err := callOllama(req2)
		if err != nil {
			log.Fatalf("Error generating final response: %v", err)
		}

		fmt.Printf("\n🤖 [AI Telephony Agent (Spoken HU)]:\n%s\n", finalResp.Message.Content)
	} else {
		fmt.Printf("\n🤖 [AI Telephony Agent (Spoken HU)]:\n%s\n", assistantMsg.Content)
	}
}
