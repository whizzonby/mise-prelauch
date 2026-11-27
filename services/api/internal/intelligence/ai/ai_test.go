package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fake(t *testing.T, h http.HandlerFunc) *OpenAI {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &OpenAI{APIKey: "sk-test", Model: "configured-model", BaseURL: srv.URL, Timeout: 2 * time.Second}
}

func TestOpenAIToolCallsStructuredOutputAndUsage(t *testing.T) {
	var got map[string]any
	o := fake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("request %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		fmt.Fprint(w, `{"model":"configured-model-2026","choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":null,
			"tool_calls":[{"id":"call_1","type":"function","function":{"name":"financial_summary","arguments":"{\"from\":\"2026-11-01\"}"}}]}}],
			"usage":{"prompt_tokens":120,"completion_tokens":30}}`)
	})
	res, err := o.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleSystem, Content: "s"}, {Role: RoleUser, Content: "q"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c0", Name: "x", Arguments: json.RawMessage(`{}`)}}},
			{Role: RoleTool, ToolCallID: "c0", Content: `{"ok":true}`}},
		Tools:     []ToolSpec{{Name: "financial_summary", Description: "d", Parameters: map[string]any{"type": "object"}}},
		Output:    &Schema{Name: "answer", Schema: map[string]any{"type": "object"}},
		MaxTokens: 500,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Model != "configured-model-2026" || res.Usage != (Usage{120, 30}) || len(res.ToolCalls) != 1 ||
		res.ToolCalls[0].Name != "financial_summary" || string(res.ToolCalls[0].Arguments) != `{"from":"2026-11-01"}` {
		t.Fatalf("response = %+v", res)
	}
	// The request: the configured model, tools, strict schema, token cap, tool messages.
	if got["model"] != "configured-model" || got["max_completion_tokens"] != float64(500) {
		t.Fatalf("body = %v", got)
	}
	rf := got["response_format"].(map[string]any)["json_schema"].(map[string]any)
	if rf["strict"] != true || rf["name"] != "answer" {
		t.Fatalf("response_format = %v", rf)
	}
	msgs := got["messages"].([]any)
	if msgs[2].(map[string]any)["content"] != nil || msgs[3].(map[string]any)["tool_call_id"] != "c0" {
		t.Fatalf("messages = %v", msgs)
	}
	if got["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"] != "financial_summary" {
		t.Fatalf("tools = %v", got["tools"])
	}
}

func TestOpenAIStreaming(t *testing.T) {
	o := fake(t, func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		if b["stream"] != true || b["stream_options"].(map[string]any)["include_usage"] != true {
			t.Errorf("stream flags = %v", b)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, d := range []string{
			`{"model":"m","choices":[{"delta":{"content":"Net "}}]}`,
			`{"choices":[{"delta":{"content":"collections"}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_9","function":{"name":"business_overview","arguments":"{\"fr"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"om\":\"2026-11-01\"}"}}]},"finish_reason":"tool_calls"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":4}}`,
			`[DONE]`,
		} {
			fmt.Fprintf(w, "data: %s\n\n", d)
		}
	})
	var parts []string
	res, err := o.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "q"}}, OnDelta: func(s string) { parts = append(parts, s) }})
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != "Net collections" || strings.Join(parts, "|") != "Net |collections" || res.Usage != (Usage{10, 4}) ||
		res.FinishReason != "tool_calls" || len(res.ToolCalls) != 1 || string(res.ToolCalls[0].Arguments) != `{"from":"2026-11-01"}` || res.ToolCalls[0].ID != "call_9" {
		t.Fatalf("stream result = %+v", res)
	}
}

func TestOpenAIStreamCutShortIsAnError(t *testing.T) {
	o := fake(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Half\"}}]}\n\n")
	})
	_, err := o.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "q"}}, OnDelta: func(string) {}})
	var pe *Error
	if !errors.As(err, &pe) || pe.Code != "bad_response" {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenAIErrorsAndRetryPolicy(t *testing.T) {
	var n atomic.Int32
	o := fake(t, func(w http.ResponseWriter, r *http.Request) {
		switch n.Add(1) {
		case 1:
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":{"message":"slow down","type":"requests","code":"rate_limit_exceeded"}}`)
		case 2:
			w.WriteHeader(http.StatusBadGateway)
		default:
			fmt.Fprint(w, `{"model":"m","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
		}
	})
	r := Retrying{P: o, Attempts: 3, Backoff: time.Millisecond}
	res, err := r.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "q"}}})
	if err != nil || res.Content != "ok" || n.Load() != 3 {
		t.Fatalf("retry: %v %v after %d calls", res, err, n.Load())
	}

	// Not retried: a bad request, and exhausted quota.
	for _, c := range []struct {
		status int
		body   string
		code   string
	}{
		{400, `{"error":{"message":"bad schema","type":"invalid_request_error","code":null}}`, "invalid_request_error"},
		{429, `{"error":{"message":"quota","type":"insufficient_quota","code":"insufficient_quota"}}`, "insufficient_quota"},
		{401, `{"error":{"message":"bad key","type":"invalid_request_error","code":"invalid_api_key"}}`, "invalid_api_key"},
	} {
		var calls atomic.Int32
		o := fake(t, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.WriteHeader(c.status)
			fmt.Fprint(w, c.body)
		})
		_, err := Retrying{P: o, Attempts: 3, Backoff: time.Millisecond}.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "q"}}})
		var pe *Error
		if !errors.As(err, &pe) || pe.Status != c.status || pe.Code != c.code || pe.Retryable || calls.Load() != 1 {
			t.Fatalf("%d: err = %v after %d calls", c.status, err, calls.Load())
		}
	}
}

func TestOpenAITimeoutAndMissingConfiguration(t *testing.T) {
	o := fake(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	})
	o.Timeout = 50 * time.Millisecond
	_, err := o.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "q"}}})
	var pe *Error
	if !errors.As(err, &pe) || pe.Code != "timeout" || !pe.Retryable {
		t.Fatalf("timeout err = %v", err)
	}
	if _, err := (&OpenAI{Model: "m"}).Complete(context.Background(), Request{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("no key: %v", err)
	}
	if _, err := (&OpenAI{APIKey: "k"}).Complete(context.Background(), Request{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("no model: %v", err)
	}
}

func TestMockIsScripted(t *testing.T) {
	m := &Mock{Script: func(call int, req Request) (*Response, error) {
		return &Response{Content: fmt.Sprintf("reply %d", call)}, nil
	}}
	var streamed string
	res, _ := m.Complete(context.Background(), Request{OnDelta: func(s string) { streamed += s }})
	res2, _ := m.Complete(context.Background(), Request{})
	if res.Content != "reply 1" || res2.Content != "reply 2" || streamed != "reply 1" || m.Calls() != 2 {
		t.Fatalf("mock: %v %v %q", res, res2, streamed)
	}
}
