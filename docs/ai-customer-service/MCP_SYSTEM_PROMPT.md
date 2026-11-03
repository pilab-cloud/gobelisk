# MCP Integration & System Prompt Engineering

This guide details how the PBX application acts as an **MCP Host / Orchestrator**, interfacing with external MCP servers (databases, CRM, ERP, logistics APIs), exposing tools to the local LLM, and handling the ReAct conversational execution loop.

---

## 1. Principles: How MCP Connects to the LLM

The **Model Context Protocol (MCP)** is an open standard operating over **JSON-RPC 2.0** (typically via `stdio` or `HTTP/SSE`).

The LLM never directly opens network connections to MCP servers. Instead, the Go application coordinates communication:

```
+---------------+                 +---------------+                 +------------+
| Customer Call |                 |  Go PBX Host  |                 | MCP Server |
| (Whisper STT) |                 | (Orchestrator)|                 | (DB / CRM) |
+-------+-------+                 +-------+-------+                 +-----+------+
        |                                 |                               |
        | 1. Hungarian speech audio       |                               |
        +-------------------------------->| 2. JSON-RPC: tools/list       |
        |                                 +------------------------------>|
        |                                 |<------------------------------+
        |                                 | 3. English tool schemas       |
        |                                 |                               |
        |                                 | 4. Ollama Chat(messages, tools)
        |                                 |    (System Prompt + User transcript)
        |                                 |                               |
        |                                 | 5. LLM tool_calls response:   |
        |                                 |    name: "create_freight_order"
        |                                 |    args: { pickup: "Budapest" }
        |                                 |                               |
        |                                 | 6. JSON-RPC: tools/call       |
        |                                 +------------------------------>|
        |                                 |<------------------------------+
        |                                 | 7. Execution result (JSON)    |
        |                                 |                               |
        |                                 | 8. Ollama Feed (role: "tool") |
        |                                 |    Final Hungarian speech text|
        | 9. Audio streamed via TTS       |                               |
        |<--------------------------------+                               |
```

---

## 2. System Prompt Engineering

To ensure robust telephony agent performance, the system prompt must strictly establish:
1. **Linguistic separation:** The conversation with the caller is in Hungarian, but all tool names, parameters, and schemas are strictly English.
2. **Deterministic tool invocation:** Call tools immediately when sufficient arguments are present; ask clarifying questions if mandatory parameters are missing.
3. **Natural speech synthesis synthesis:** Never read out raw JSON to the user; summarize results in friendly, natural conversational Hungarian.

### Standard Telephony System Prompt Template:

```text
Te egy magyar nyelvű telefonos ügyfélszolgálati asszisztens vagy a PBX rendszerben.

ALAPVETŐ SZABÁLYOK (CORE RULES):
1. LANGUAGES:
   - Always communicate with the caller in polite, concise, natural spoken Hungarian suitable for telephony.
   - All external tools, function names, parameter names, and schemas are strictly in English.

2. TOOL USAGE (MCP / FUNCTIONS):
   - When caller intent matches an available tool (e.g. freight ordering, invoice lookup, incident ticketing, call transfer), call the tool immediately with extracted parameters.
   - Do not hallucinate mandatory data: if a required parameter is missing, ask the user in Hungarian rather than submitting invalid arguments.
   - Once mandatory parameters are gathered, trigger the tool call.

3. RESULT FEEDBACK & SPEECH SYNTHESIS:
   - When a tool returns its output JSON, summarize the result in concise, friendly Hungarian sentences (e.g. confirmation of booking, ticket ID, invoice due date).
   - Never output raw JSON, technical stack traces, or internal IDs to the user; convert technical status messages into natural spoken phrasing.
```

---

## 3. Tool Schema Mapping: MCP to Ollama

MCP schemas use standard JSON Schema definitions inside `inputSchema`. This maps directly to Ollama/OpenAI tool representations without transformation overhead:

### MCP Server (`tools/list` response):
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "tools": [
      {
        "name": "lookup_customer",
        "description": "Retrieve customer account by phone number or tax ID.",
        "inputSchema": {
          "type": "object",
          "properties": {
            "phone_number": {"type": "string", "description": "E.164 formatted phone number"},
            "tax_id": {"type": "string", "description": "National tax identification number"}
          }
        }
      }
    ]
  }
}
```

### Go Bridge Conversion:
```go
ollamaTool := OllamaTool{
    Type: "function",
}
ollamaTool.Function.Name = mcpTool.Name
ollamaTool.Function.Description = mcpTool.Description
ollamaTool.Function.Parameters = mcpTool.InputSchema
```

### Tool Execution Dispatch (`tools/call`):
When the LLM decides to invoke the tool:
```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/call",
  "params": {
    "name": "lookup_customer",
    "arguments": {
      "phone_number": "+36301234567"
    }
  }
}
```

The response is appended back into the message history with `role: "tool"`, completing the ReAct feedback cycle.
