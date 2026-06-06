package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSplitReasoning(t *testing.T) {
	tests := []struct {
		name              string
		msg               ResponseMessage
		wantC, wantReason string
	}{
		{"reasoning_content field", ResponseMessage{Content: "answer", ReasoningContent: "thinking"}, "answer", "thinking"},
		{"inline think", ResponseMessage{Content: "<think>why</think>final"}, "final", "why"},
		{"no think", ResponseMessage{Content: "plain"}, "plain", ""},
		{"unterminated think", ResponseMessage{Content: "pre<think>cut off"}, "pre", "cut off"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotC, gotR := SplitReasoning(tt.msg)
			if gotC != tt.wantC || gotR != tt.wantReason {
				t.Fatalf("got (%q,%q) want (%q,%q)", gotC, gotR, tt.wantC, tt.wantReason)
			}
		})
	}
}

func TestMergeToolCall(t *testing.T) {
	var tcs []ToolCall
	mergeToolCall(&tcs, toolCallDelta{Index: 0, ID: "call_1", Type: "function"})
	mergeToolCall(&tcs, frag(0, `{"path":`))
	mergeToolCall(&tcs, frag(0, `"a.go"}`))
	if len(tcs) != 1 {
		t.Fatalf("want 1 tool call, got %d", len(tcs))
	}
	if tcs[0].ID != "call_1" {
		t.Errorf("id = %q", tcs[0].ID)
	}
	if got := tcs[0].Function.Arguments; got != `{"path":"a.go"}` {
		t.Errorf("args = %q", got)
	}
}

func frag(i int, args string) toolCallDelta {
	d := toolCallDelta{Index: i}
	d.Function.Arguments = args
	return d
}

func TestComplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"total_tokens":7}}`))
	}))
	defer srv.Close()

	resp, err := NewClient(srv.URL, "m").Complete(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Choices[0].Message.Content != "hi" || resp.Usage.TotalTokens != 7 {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

// TestRetryableTransportError covers the transport leg of the retry
// classification — the 5xx and 4xx legs are exercised in the agent package. A
// request to a closed endpoint must surface a Retryable error so the Ralph loop
// retries a momentarily-unreachable server instead of failing the pass outright.
func TestRetryableTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // the endpoint now refuses connections

	_, err := NewClient(url, "m").Complete(context.Background(), Request{})
	if err == nil {
		t.Fatal("Complete against a closed endpoint should error")
	}
	if !Retryable(err) {
		t.Errorf("transport error must be Retryable, got %v", err)
	}
}

func TestCompleteStream(t *testing.T) {
	frames := []string{
		`data: {"choices":[{"delta":{"role":"assistant"}}]}`,
		`data: {"choices":[{"delta":{"reasoning_content":"th"}}]}`,
		`data: {"choices":[{"delta":{"reasoning_content":"ink"}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"write_file","arguments":"{\"path\":"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a.go\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"total_tokens":42}}`,
		`data: [DONE]`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for _, f := range frames {
			_, _ = w.Write([]byte(f + "\n\n"))
		}
	}))
	defer srv.Close()

	var streamed string
	resp, err := NewClient(srv.URL, "m").CompleteStream(context.Background(), Request{}, func(_, text string) { streamed += text })
	if err != nil {
		t.Fatal(err)
	}
	msg := resp.Choices[0].Message
	if msg.ReasoningContent != "think" || streamed != "think" {
		t.Errorf("reasoning=%q streamed=%q", msg.ReasoningContent, streamed)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Arguments != `{"path":"a.go"}` {
		t.Errorf("tool calls = %+v", msg.ToolCalls)
	}
	if resp.Choices[0].FinishReason != "tool_calls" || resp.Usage.TotalTokens != 42 {
		t.Errorf("finish=%q usage=%d", resp.Choices[0].FinishReason, resp.Usage.TotalTokens)
	}
}
