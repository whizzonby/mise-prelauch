// Package ai is the language-model provider abstraction for Mise
// Intelligence. A Provider completes a chat with optional tool calling,
// structured (JSON-schema) output and streaming, and reports token usage.
//
// Nothing here knows about Mise data or permissions: the caller decides which
// tools a model may see and runs every tool call itself, with the user's own
// permissions. Credentials stay in this process; they are never stored or
// sent anywhere but the provider.
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Roles.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Message is one chat message.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall is a model's request to run a tool.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolSpec describes a tool the model may call.
type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// Schema asks for structured output matching a JSON schema.
type Schema struct {
	Name   string         `json:"name"`
	Schema map[string]any `json:"schema"`
}

// Request is one completion request.
type Request struct {
	Model     string
	Messages  []Message
	Tools     []ToolSpec
	Output    *Schema // nil: free text
	MaxTokens int
	// OnDelta, when set, streams the reply: it receives each text fragment
	// as it arrives. The full reply is still returned.
	OnDelta func(string)
}

// Usage is the token usage a provider reported.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Response is a completion.
type Response struct {
	Model        string
	Content      string
	ToolCalls    []ToolCall
	FinishReason string
	Usage        Usage
}

// Provider is a language-model provider.
type Provider interface {
	Name() string
	Complete(ctx context.Context, req Request) (*Response, error)
}

// Error is a provider failure. Retryable errors (rate limits, server errors,
// timeouts) may succeed when tried again.
type Error struct {
	Status    int
	Code      string
	Message   string
	Retryable bool
}

func (e *Error) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("ai provider: %d %s: %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("ai provider: %s: %s", e.Code, e.Message)
}

// ErrNotConfigured: no provider is configured.
var ErrNotConfigured = errors.New("ai provider: not configured")

// Retrying wraps a provider with a retry policy: retryable errors are tried
// again up to Attempts times in all, waiting Backoff, then twice as long.
type Retrying struct {
	P        Provider
	Attempts int
	Backoff  time.Duration
}

func (r Retrying) Name() string { return r.P.Name() }

func (r Retrying) Complete(ctx context.Context, req Request) (*Response, error) {
	attempts := max(r.Attempts, 1)
	wait := r.Backoff
	var err error
	for i := 0; i < attempts; i++ {
		var res *Response
		if res, err = r.P.Complete(ctx, req); err == nil {
			return res, nil
		}
		var pe *Error
		if !errors.As(err, &pe) || !pe.Retryable || i == attempts-1 || req.OnDelta != nil {
			// A streamed reply is not retried: part of it was already shown.
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		wait *= 2
	}
	return nil, err
}

// Mock is a scripted provider for tests: Script returns each reply.
type Mock struct {
	Script func(call int, req Request) (*Response, error)
	calls  int
}

func (m *Mock) Name() string { return "mock" }

func (m *Mock) Complete(ctx context.Context, req Request) (*Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.calls++
	res, err := m.Script(m.calls, req)
	if err == nil && req.OnDelta != nil && res.Content != "" {
		req.OnDelta(res.Content)
	}
	return res, err
}

// Calls is how many completions the mock served.
func (m *Mock) Calls() int { return m.calls }
