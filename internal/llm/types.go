// Package llm holds the data types and HTTP client for an OpenAI-compatible
// chat-completions endpoint (here, a local oMLX server serving Qwen3.6).
package llm

// Chat roles.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Message is one chat-history entry sent to or received from the model. The
// thinking ("reasoning") trace is deliberately absent: it is logged for
// observability but never stored here, per Qwen's multi-turn contract and to
// conserve the context window.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall is a function invocation requested by the model.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall names a tool and carries its arguments as a JSON-encoded object.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool is a function exposed to the model via native function-calling.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction describes a callable tool; Parameters is a JSON Schema object.
type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// Sampling holds the decoding parameters pinned for Qwen3.6 thinking mode.
type Sampling struct {
	MaxTokens         int
	Temperature       float64
	TopP              float64
	TopK              int
	MinP              float64
	RepetitionPenalty float64
	PresencePenalty   float64
}

// StreamOptions asks the server for streaming extras. IncludeUsage requests a
// final usage frame so the context guard still works while streaming.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// Request is the body of a chat-completions call.
type Request struct {
	Model             string         `json:"model"`
	Messages          []Message      `json:"messages"`
	Tools             []Tool         `json:"tools,omitempty"`
	Stream            bool           `json:"stream"`
	StreamOptions     *StreamOptions `json:"stream_options,omitempty"`
	MaxTokens         int            `json:"max_tokens,omitempty"`
	Temperature       float64        `json:"temperature"`
	TopP              float64        `json:"top_p"`
	TopK              int            `json:"top_k,omitempty"`
	MinP              float64        `json:"min_p"`
	RepetitionPenalty float64        `json:"repetition_penalty,omitempty"`
	PresencePenalty   float64        `json:"presence_penalty,omitempty"`
}

// Response is the subset of the chat-completions reply we consume.
type Response struct {
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice is one completion alternative.
type Choice struct {
	Message      ResponseMessage `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

// ResponseMessage is the assistant message returned by the model. Some servers
// place the thinking trace in ReasoningContent; others inline it in Content as
// <think>…</think>. SplitReasoning normalises both.
type ResponseMessage struct {
	Role             string     `json:"role"`
	Content          string     `json:"content"`
	ReasoningContent string     `json:"reasoning_content"`
	ToolCalls        []ToolCall `json:"tool_calls"`
}

// Usage reports token accounting for a single call.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
