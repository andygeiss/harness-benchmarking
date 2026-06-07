package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Client calls an OpenAI-compatible chat-completions endpoint.
type Client struct {
	baseURL string
	model   string
	http    *http.Client
}

// NewClient returns a Client for the given base URL (e.g. http://localhost:1234/v1)
// and model name. It sets no HTTP timeout: a long thinking completion is bounded
// by the caller's context instead.
func NewClient(baseURL, model string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		http:    &http.Client{},
	}
}

// transientError marks a failure that may succeed on retry: a transport error,
// a truncated response body, or a 5xx status. Retryable reports it.
type transientError struct{ err error }

func (e *transientError) Error() string { return e.err.Error() }
func (e *transientError) Unwrap() error { return e.err }

func transient(err error) error { return &transientError{err: err} }

// Retryable reports whether err is a transient failure worth retrying.
func Retryable(err error) bool {
	var t *transientError
	return errors.As(err, &t)
}

// post marshals req and issues the chat-completions POST. The caller owns the body.
func (c *Client) post(ctx context.Context, req Request) (*http.Response, error) {
	req.Model = c.model
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	return c.http.Do(httpReq)
}

// Complete sends one non-streaming request and returns the parsed response.
func (c *Client) Complete(ctx context.Context, req Request) (*Response, error) {
	resp, err := c.post(ctx, req)
	if err != nil {
		return nil, transient(fmt.Errorf("call endpoint: %w", err))
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, transient(fmt.Errorf("read response: %w", err))
	}
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("endpoint returned %s: %s", resp.Status, strings.TrimSpace(string(data)))
		if resp.StatusCode >= 500 {
			return nil, transient(err)
		}
		return nil, err
	}
	var out Response
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}

// CompleteStream sends a streaming request, invoking onDelta for each fragment of
// reasoning ("reasoning") and answer ("content") as it arrives, and returns the
// fully assembled response — identical in shape to Complete — once [DONE] is
// seen. onDelta may be nil.
func (c *Client) CompleteStream(ctx context.Context, req Request, onDelta func(kind, text string)) (*Response, error) {
	if onDelta == nil {
		onDelta = func(string, string) {} // normalize once so the accumulation loop need not nil-check
	}
	req.Stream = true
	req.StreamOptions = &StreamOptions{IncludeUsage: true}
	resp, err := c.post(ctx, req)
	if err != nil {
		return nil, transient(fmt.Errorf("call endpoint: %w", err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("endpoint returned %s: %s", resp.Status, strings.TrimSpace(string(data)))
		if resp.StatusCode >= 500 {
			return nil, transient(err)
		}
		return nil, err
	}

	st, err := readStream(resp.Body, onDelta)
	if err != nil {
		return nil, transient(fmt.Errorf("read stream: %w", err))
	}
	// A clean EOF that arrived before the [DONE] sentinel AND before any
	// finish_reason means the body was cut mid-response: bufio.Scanner reports it as
	// success, so without this check a half-generated turn would be accepted as
	// complete. Treat it as a truncated read (transient) so the pass retries instead
	// of acting on partial output. A stream that did reach a finish_reason kept its
	// content, so a merely-missing trailing [DONE] frame is tolerated.
	if !st.sawDone && st.finish == "" {
		return nil, transient(fmt.Errorf("stream ended before [DONE] or a finish reason: response truncated"))
	}

	return &Response{
		Choices: []Choice{{
			Message: ResponseMessage{
				Role:             RoleAssistant,
				Content:          st.content,
				ReasoningContent: st.reasoning,
				ToolCalls:        st.toolCalls,
			},
			FinishReason: st.finish,
		}},
		Usage: st.usage,
	}, nil
}

// streamState is the response assembled incrementally from SSE delta frames.
type streamState struct {
	content   string
	reasoning string
	toolCalls []ToolCall
	finish    string
	usage     Usage
	sawDone   bool // the [DONE] sentinel was seen (the stream ended cleanly)
}

// readStream scans an SSE body into a streamState, invoking onDelta for each
// fragment of reasoning and answer as it arrives. It stops at [DONE] (setting
// sawDone) or at EOF; the caller treats an EOF before [DONE] as a truncated read.
func readStream(body io.Reader, onDelta func(kind, text string)) (streamState, error) {
	var content, reasoning strings.Builder
	var st streamState
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20) // tolerate long SSE lines
	for sc.Scan() {
		chunk, ok, done := parseFrame(sc.Text())
		if done {
			st.sawDone = true
			break
		}
		if !ok {
			continue
		}
		if chunk.Usage != nil {
			st.usage = *chunk.Usage
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		ch := chunk.Choices[0]
		if ch.Delta.ReasoningContent != "" {
			reasoning.WriteString(ch.Delta.ReasoningContent)
			onDelta("reasoning", ch.Delta.ReasoningContent)
		}
		if ch.Delta.Content != "" {
			content.WriteString(ch.Delta.Content)
			onDelta("content", ch.Delta.Content)
		}
		for _, tc := range ch.Delta.ToolCalls {
			mergeToolCall(&st.toolCalls, tc)
		}
		if ch.FinishReason != "" {
			st.finish = ch.FinishReason
		}
	}
	st.content = content.String()
	st.reasoning = reasoning.String()
	return st, sc.Err()
}

// parseFrame extracts the JSON chunk from one SSE line. ok is false for lines to
// skip (non-data lines, keepalive primers, malformed JSON); done is true at the
// terminating "data: [DONE]" sentinel.
func parseFrame(line string) (chunk streamChunk, ok, done bool) {
	data, hasData := strings.CutPrefix(line, "data: ")
	if !hasData {
		return chunk, false, false
	}
	if data == "[DONE]" {
		return chunk, false, true
	}
	if json.Unmarshal([]byte(data), &chunk) != nil {
		return chunk, false, false
	}
	return chunk, true, false
}

// streamChunk is one SSE delta frame.
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content          string          `json:"content"`
			ReasoningContent string          `json:"reasoning_content"`
			ToolCalls        []toolCallDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage `json:"usage"`
}

// toolCallDelta is a streamed fragment of a tool call, addressed by Index.
type toolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// mergeToolCall folds a streamed fragment into dst, accumulating argument text
// per Index (the server may split one call's arguments across many frames).
func mergeToolCall(dst *[]ToolCall, d toolCallDelta) {
	if d.Index < 0 { // a malformed frame must not panic the whole run
		return
	}
	for len(*dst) <= d.Index {
		*dst = append(*dst, ToolCall{Type: "function"})
	}
	tc := &(*dst)[d.Index]
	if d.ID != "" {
		tc.ID = d.ID
	}
	if d.Type != "" {
		tc.Type = d.Type
	}
	if d.Function.Name != "" {
		tc.Function.Name = d.Function.Name
	}
	tc.Function.Arguments += d.Function.Arguments
}

// SplitReasoning separates the model's answer from its thinking trace, handling
// both the reasoning_content field and inline <think>…</think> tags. The
// reasoning is returned for logging only and must not be stored in history.
func SplitReasoning(m ResponseMessage) (content, reasoning string) {
	if m.ReasoningContent != "" {
		return strings.TrimSpace(m.Content), strings.TrimSpace(m.ReasoningContent)
	}
	const openTag, closeTag = "<think>", "</think>"
	i := strings.Index(m.Content, openTag)
	if i < 0 {
		return m.Content, ""
	}
	j := strings.Index(m.Content, closeTag)
	if j < 0 {
		// Truncated thinking (hit max_tokens before closing): the rest is reasoning.
		return strings.TrimSpace(m.Content[:i]), strings.TrimSpace(m.Content[i+len(openTag):])
	}
	reasoning = strings.TrimSpace(m.Content[i+len(openTag) : j])
	content = strings.TrimSpace(m.Content[:i] + m.Content[j+len(closeTag):])
	return content, reasoning
}
