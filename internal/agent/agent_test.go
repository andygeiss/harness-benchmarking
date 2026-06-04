package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"harness/internal/llm"
	"harness/internal/tool"
)

// TestRunCompletes drives the full inner loop against a scripted server: the
// model first calls a stub tool, then calls done; with a passing verifier the
// session must end as completed.
func TestRunCompletes(t *testing.T) {
	responses := []string{
		`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"noop","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"total_tokens":10}}`,
		`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c2","type":"function","function":{"name":"done","arguments":"{\"summary\":\"ok\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"total_tokens":20}}`,
	}
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		i := min(n, len(responses)-1)
		n++
		_, _ = w.Write([]byte(responses[i]))
	}))
	defer srv.Close()

	var noopCalled bool
	reg := tool.NewRegistry()
	reg.Register(tool.Tool{
		Name:   "noop",
		Schema: map[string]any{"type": "object"},
		Run: func(_ context.Context, _ json.RawMessage) (string, error) {
			noopCalled = true
			return "ok", nil
		},
	})
	reg.Register(tool.Done(func(_ context.Context) (bool, string, error) {
		return true, "", nil
	}))

	sess := NewSession(
		llm.NewClient(srv.URL, "test"),
		reg,
		llm.Sampling{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config{MaxSteps: 5},
	)
	res, err := sess.Run(context.Background(), "sys", "do it")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Completed || res.Reason != "completed" {
		t.Fatalf("want completed, got %+v", res)
	}
	if !noopCalled {
		t.Error("noop tool was not called")
	}
}

// TestUnknownToolErrorListsTools checks the recovery hint: a call to an
// unregistered tool (here a malformed name) is fed back with the valid tool
// names so the model can correct itself instead of looping.
func TestUnknownToolErrorListsTools(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register(tool.Tool{Name: "alpha", Schema: map[string]any{"type": "object"}})
	reg.Register(tool.Tool{Name: "beta", Schema: map[string]any{"type": "object"}})
	s := NewSession(nil, reg, llm.Sampling{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{})

	res, completed := s.runTool(context.Background(), llm.ToolCall{
		Function: llm.FunctionCall{Name: "go\n<parameter=args", Arguments: "{}"},
	})
	if completed {
		t.Fatal("unknown tool must not signal completion")
	}
	for _, want := range []string{"unknown tool", "alpha", "beta"} {
		if !strings.Contains(res, want) {
			t.Errorf("error message %q missing %q", res, want)
		}
	}
}

// TestCompleteRetriesTransient drives complete against a server that returns two
// 5xx responses before succeeding; the session must retry through them.
func TestCompleteRetriesTransient(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n++
		if n <= 2 {
			http.Error(w, "overloaded", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"total_tokens":1}}`))
	}))
	defer srv.Close()

	s := NewSession(llm.NewClient(srv.URL, "m"), tool.NewRegistry(), llm.Sampling{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{})
	resp, err := s.complete(context.Background(), llm.Request{})
	if err != nil {
		t.Fatalf("complete after retries: %v", err)
	}
	if resp.Choices[0].Message.Content != "ok" {
		t.Fatalf("content = %q", resp.Choices[0].Message.Content)
	}
	if n != 3 {
		t.Errorf("server hit %d times, want 3 (two retries)", n)
	}
}

// TestCompleteNoRetryOn4xx confirms a client error is permanent: it is returned
// after a single attempt, not retried.
func TestCompleteNoRetryOn4xx(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n++
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	s := NewSession(llm.NewClient(srv.URL, "m"), tool.NewRegistry(), llm.Sampling{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{})
	if _, err := s.complete(context.Background(), llm.Request{}); err == nil {
		t.Fatal("expected error on 400")
	}
	if n != 1 {
		t.Errorf("server hit %d times, want 1 (no retry on 4xx)", n)
	}
}

// TestDoneLoopEndsPass ends a pass once done keeps failing verification with no
// intervening file change, instead of consuming the whole step budget.
func TestDoneLoopEndsPass(t *testing.T) {
	doneCall := `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c","type":"function","function":{"name":"done","arguments":"{\"summary\":\"x\"}"}}]}}],"usage":{"total_tokens":1}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(doneCall))
	}))
	defer srv.Close()

	reg := tool.NewRegistry()
	reg.Register(tool.Done(func(_ context.Context) (bool, string, error) {
		return false, "still failing", nil // verification never passes
	}))

	sess := NewSession(llm.NewClient(srv.URL, "m"), reg, llm.Sampling{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{MaxSteps: 20})
	res, err := sess.Run(context.Background(), "sys", "go")
	if err != nil {
		t.Fatal(err)
	}
	if res.Reason != "done_loop" {
		t.Fatalf("reason = %q, want done_loop", res.Reason)
	}
	if res.Steps != maxDoneFails {
		t.Errorf("steps = %d, want %d (early-out at the loop threshold)", res.Steps, maxDoneFails)
	}
}
