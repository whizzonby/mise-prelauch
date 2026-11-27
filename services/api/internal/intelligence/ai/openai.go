package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// OpenAI is the OpenAI Chat Completions adapter (tools, structured output,
// streaming, usage). It was built to OpenAI's API reference and tested
// against a local fake server; it has not been verified against OpenAI
// itself (no key was provided). The model is configuration, never code.
type OpenAI struct {
	APIKey  string
	Model   string
	BaseURL string // default https://api.openai.com/v1
	Timeout time.Duration
	Client  *http.Client
}

func (o *OpenAI) Name() string { return "openai" }

type oaToolCall struct {
	Index    *int   `json:"index,omitempty"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type oaMessage struct {
	Role       string       `json:"role"`
	Content    *string      `json:"content"`
	ToolCalls  []oaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
}

type oaUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

func (o *OpenAI) body(req Request, stream bool) map[string]any {
	msgs := make([]oaMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		content := m.Content
		om := oaMessage{Role: m.Role, Content: &content, ToolCallID: m.ToolCallID}
		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 && content == "" {
			om.Content = nil
		}
		for _, tc := range m.ToolCalls {
			x := oaToolCall{ID: tc.ID, Type: "function"}
			x.Function.Name, x.Function.Arguments = tc.Name, string(tc.Arguments)
			om.ToolCalls = append(om.ToolCalls, x)
		}
		msgs = append(msgs, om)
	}
	model := req.Model
	if model == "" {
		model = o.Model
	}
	b := map[string]any{"model": model, "messages": msgs}
	if req.MaxTokens > 0 {
		b["max_completion_tokens"] = req.MaxTokens
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{"type": "function",
				"function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Parameters}})
		}
		b["tools"] = tools
	}
	if req.Output != nil {
		b["response_format"] = map[string]any{"type": "json_schema",
			"json_schema": map[string]any{"name": req.Output.Name, "schema": req.Output.Schema, "strict": true}}
	}
	if stream {
		b["stream"] = true
		b["stream_options"] = map[string]any{"include_usage": true}
	}
	return b
}

func (o *OpenAI) Complete(ctx context.Context, req Request) (*Response, error) {
	if o.APIKey == "" || o.Model == "" && req.Model == "" {
		return nil, ErrNotConfigured
	}
	if o.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.Timeout)
		defer cancel()
	}
	stream := req.OnDelta != nil
	payload, err := json.Marshal(o.body(req, stream))
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(o.BaseURL, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	hr.Header.Set("Authorization", "Bearer "+o.APIKey)
	hr.Header.Set("Content-Type", "application/json")
	client := o.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(hr)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, &Error{Code: "timeout", Message: "the provider did not answer in time", Retryable: true}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &Error{Code: "network", Message: "the provider could not be reached", Retryable: true}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, oaError(resp)
	}
	if stream {
		return readStream(resp.Body, req.OnDelta)
	}
	var out struct {
		Model   string `json:"model"`
		Choices []struct {
			Message      oaMessage `json:"message"`
			FinishReason string    `json:"finish_reason"`
		} `json:"choices"`
		Usage oaUsage `json:"usage"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return nil, &Error{Code: "bad_response", Message: "the provider's reply could not be read"}
	}
	if len(out.Choices) == 0 {
		return nil, &Error{Code: "bad_response", Message: "the provider returned no choices"}
	}
	c := out.Choices[0]
	res := &Response{Model: out.Model, FinishReason: c.FinishReason,
		Usage: Usage{InputTokens: out.Usage.PromptTokens, OutputTokens: out.Usage.CompletionTokens}}
	if c.Message.Content != nil {
		res.Content = *c.Message.Content
	}
	for _, tc := range c.Message.ToolCalls {
		res.ToolCalls = append(res.ToolCalls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: rawArgs(tc.Function.Arguments)})
	}
	return res, nil
}

func rawArgs(s string) json.RawMessage {
	if strings.TrimSpace(s) == "" {
		return json.RawMessage("{}")
	}
	return json.RawMessage(s)
}

func oaError(resp *http.Response) error {
	var e struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    any    `json:"code"`
		} `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&e)
	code := e.Error.Type
	if s, ok := e.Error.Code.(string); ok && s != "" {
		code = s
	}
	if code == "" {
		code = "http_error"
	}
	msg := e.Error.Message
	if len(msg) > 300 {
		msg = msg[:300]
	}
	// Rate limits and server errors may pass; quota exhaustion does not.
	retry := resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests && code != "insufficient_quota"
	return &Error{Status: resp.StatusCode, Code: code, Message: msg, Retryable: retry}
}

// readStream reads a server-sent-events completion.
func readStream(body io.Reader, onDelta func(string)) (*Response, error) {
	res := &Response{}
	var text strings.Builder
	calls := map[int]*ToolCall{}
	args := map[int]*strings.Builder{}
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	done := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			done = true
			break
		}
		var chunk struct {
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					Content   string       `json:"content"`
					ToolCalls []oaToolCall `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *oaUsage `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return nil, &Error{Code: "bad_response", Message: "the provider's stream could not be read"}
		}
		if chunk.Model != "" {
			res.Model = chunk.Model
		}
		if chunk.Usage != nil {
			res.Usage = Usage{InputTokens: chunk.Usage.PromptTokens, OutputTokens: chunk.Usage.CompletionTokens}
		}
		for _, c := range chunk.Choices {
			if c.Delta.Content != "" {
				text.WriteString(c.Delta.Content)
				onDelta(c.Delta.Content)
			}
			for _, tc := range c.Delta.ToolCalls {
				i := 0
				if tc.Index != nil {
					i = *tc.Index
				}
				if calls[i] == nil {
					calls[i], args[i] = &ToolCall{}, &strings.Builder{}
				}
				if tc.ID != "" {
					calls[i].ID = tc.ID
				}
				if tc.Function.Name != "" {
					calls[i].Name = tc.Function.Name
				}
				args[i].WriteString(tc.Function.Arguments)
			}
			if c.FinishReason != nil {
				res.FinishReason = *c.FinishReason
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, &Error{Code: "network", Message: fmt.Sprintf("the provider's stream was interrupted: %v", err), Retryable: true}
	}
	if !done {
		return nil, &Error{Code: "bad_response", Message: "the provider's stream ended early", Retryable: true}
	}
	res.Content = text.String()
	idx := make([]int, 0, len(calls))
	for i := range calls {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	for _, i := range idx {
		c := calls[i]
		c.Arguments = rawArgs(args[i].String())
		res.ToolCalls = append(res.ToolCalls, *c)
	}
	return res, nil
}
