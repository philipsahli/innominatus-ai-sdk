package llm

// Config holds LLM client configuration
type Config struct {
	Provider    string
	APIKey      string
	Model       string
	Temperature float32
	MaxTokens   int
}

// GenerateRequest represents a request to generate text
type GenerateRequest struct {
	SystemPrompt string
	UserPrompt   string
	Temperature  float32
	MaxTokens    int
	Tools        []Tool // Optional tools for function calling
}

// GenerateResponse represents the response from the LLM
type GenerateResponse struct {
	Text       string
	Usage      Usage
	ToolUses   []ToolUse // Tool use requests from the LLM
	StopReason string    // Why generation stopped (end_turn, tool_use, etc.)
}

// Usage tracks token usage
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// Tool represents a function that the LLM can call
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// ToolUse represents a request from the LLM to use a tool
type ToolUse struct {
	ID    string                 `json:"id"`
	Name  string                 `json:"name"`
	Input map[string]interface{} `json:"input"`
}

// ToolResult represents the result of executing a tool
type ToolResult struct {
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

// GenerateWithToolsRequest represents a request with tool use capability
type GenerateWithToolsRequest struct {
	SystemPrompt string
	Messages     []Message
	Temperature  float32
	MaxTokens    int
	Tools        []Tool
}

// Message represents a conversation message
type Message struct {
	Role    string         `json:"role"` // "user" or "assistant"
	Content []ContentBlock `json:"content"`
}

// ContentBlock represents different types of content in a message
type ContentBlock struct {
	Type      string                 `json:"type"` // "text", "tool_use", "tool_result"
	Text      string                 `json:"text,omitempty"`
	ID        string                 `json:"id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Input     map[string]interface{} `json:"input,omitempty"`
	ToolUseID string                 `json:"tool_use_id,omitempty"`
	Content   string                 `json:"content,omitempty"`
	IsError   bool                   `json:"is_error,omitempty"`
}
