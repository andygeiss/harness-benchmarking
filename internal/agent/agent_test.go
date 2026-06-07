package agent

import (
	"bytes"
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

// TestReasoningNotStoredInHistory locks the "reasoning is never stored in history"
// invariant at the loop level: an assistant turn carrying an inline <think> trace
// must be appended with only its answer, so the trace never reaches the model on a
// later step. Call 1 returns reasoning plus a tool call (forcing a second call);
// the captured body of call 2 must carry the answer but none of the reasoning.
func TestReasoningNotStoredInHistory(t *testing.T) {
	responses := []string{
		`{"choices":[{"message":{"role":"assistant","content":"<think>SECRET PLAN</think>visible answer","tool_calls":[{"id":"c1","type":"function","function":{"name":"noop","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"total_tokens":10}}`,
		`{"choices":[{"message":{"role":"assistant","content":"all done"}}],"usage":{"total_tokens":20}}`,
	}
	var bodies []string
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		i := min(n, len(responses)-1)
		n++
		_, _ = w.Write([]byte(responses[i]))
	}))
	defer srv.Close()

	reg := tool.NewRegistry()
	reg.Register(tool.Tool{
		Name:   "noop",
		Schema: map[string]any{"type": "object"},
		Run:    func(context.Context, json.RawMessage) (string, error) { return "ok", nil },
	})
	sess := NewSession(llm.NewClient(srv.URL, "m"), reg, llm.Sampling{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{MaxSteps: 5})
	if _, err := sess.Run(context.Background(), "sys", "go"); err != nil {
		t.Fatal(err)
	}

	if len(bodies) < 2 {
		t.Fatalf("want at least 2 model calls (so a prior assistant turn is re-sent), got %d", len(bodies))
	}
	second := bodies[1]
	if !strings.Contains(second, "visible answer") {
		t.Errorf("second request omits the assistant's answer:\n%s", second)
	}
	if strings.Contains(second, "SECRET PLAN") || strings.Contains(second, "<think>") {
		t.Errorf("reasoning trace leaked into history on the second request:\n%s", second)
	}
}

// TestRunEndsOnContextBudget: once a step's reported usage reaches CtxLimit the
// pass ends with reason "context", even though the step budget is far from spent.
func TestRunEndsOnContextBudget(t *testing.T) {
	toolCall := `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c","type":"function","function":{"name":"noop","arguments":"{}"}}]}}],"usage":{"total_tokens":150}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(toolCall))
	}))
	defer srv.Close()

	reg := tool.NewRegistry()
	reg.Register(tool.Tool{Name: "noop", Schema: map[string]any{"type": "object"}, Run: func(context.Context, json.RawMessage) (string, error) { return "ok", nil }})
	sess := NewSession(llm.NewClient(srv.URL, "m"), reg, llm.Sampling{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{MaxSteps: 20, CtxLimit: 100})
	res, err := sess.Run(context.Background(), "sys", "go")
	if err != nil {
		t.Fatal(err)
	}
	if res.Reason != "context" {
		t.Fatalf("reason = %q, want context", res.Reason)
	}
	if res.Steps != 1 {
		t.Errorf("steps = %d, want 1 (budget tripped after the first step)", res.Steps)
	}
}

// TestRunEndsOnMaxSteps: a model that keeps acting without completing is stopped
// by the per-pass step budget, ending with reason "max_steps".
func TestRunEndsOnMaxSteps(t *testing.T) {
	toolCall := `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c","type":"function","function":{"name":"noop","arguments":"{}"}}]}}],"usage":{"total_tokens":1}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(toolCall))
	}))
	defer srv.Close()

	reg := tool.NewRegistry()
	reg.Register(tool.Tool{Name: "noop", Schema: map[string]any{"type": "object"}, Run: func(context.Context, json.RawMessage) (string, error) { return "ok", nil }})
	sess := NewSession(llm.NewClient(srv.URL, "m"), reg, llm.Sampling{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{MaxSteps: 3})
	res, err := sess.Run(context.Background(), "sys", "go")
	if err != nil {
		t.Fatal(err)
	}
	if res.Reason != "max_steps" {
		t.Fatalf("reason = %q, want max_steps", res.Reason)
	}
	if res.Steps != 3 {
		t.Errorf("steps = %d, want 3 (the MaxSteps cap)", res.Steps)
	}
}

// TestDoneLoopResetsOnWrite covers the reset branch the done_loop guard depends on
// to NOT punish a productive model: a file-mutating call between failing done calls
// resets the counter, so three failing dones interleaved with writes never trip
// done_loop — the pass runs out the step budget instead. Without the reset the
// third done (step 5) would early-out as done_loop.
func TestDoneLoopResetsOnWrite(t *testing.T) {
	done := `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"d","type":"function","function":{"name":"done","arguments":"{\"summary\":\"x\"}"}}]}}],"usage":{"total_tokens":1}}`
	write := `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"w","type":"function","function":{"name":"write_file","arguments":"{}"}}]}}],"usage":{"total_tokens":1}}`
	responses := []string{done, write, done, write, done}
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		i := min(n, len(responses)-1)
		n++
		_, _ = w.Write([]byte(responses[i]))
	}))
	defer srv.Close()

	reg := tool.NewRegistry()
	reg.Register(tool.Done(func(context.Context) (bool, string, error) { return false, "nope", nil }))
	reg.Register(tool.Tool{Name: "write_file", Schema: map[string]any{"type": "object"}, Run: func(context.Context, json.RawMessage) (string, error) { return "ok", nil }})

	sess := NewSession(llm.NewClient(srv.URL, "m"), reg, llm.Sampling{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{MaxSteps: 5})
	res, err := sess.Run(context.Background(), "sys", "go")
	if err != nil {
		t.Fatal(err)
	}
	if res.Reason != "max_steps" {
		t.Fatalf("reason = %q, want max_steps (done_loop must not trip when writes intervene)", res.Reason)
	}
}

// TestRunRecordsToolMetrics locks the load:act instrumentation: the per-tool call
// counts and the read_file byte total folded into Result.Metrics. The model calls
// read_file (returning a known-length payload), list_dir, then write_file, then
// stops. The metrics must count each tool by name and attribute ONLY the read_file
// payload to ReadBytes — the load-vs-act signal docs/stagnation.md turns on.
func TestRunRecordsToolMetrics(t *testing.T) {
	toolCall := func(id, name string) string {
		return `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"` + id +
			`","type":"function","function":{"name":"` + name + `","arguments":"{}"}}]}}],"usage":{"total_tokens":1}}`
	}
	responses := []string{
		toolCall("r", "read_file"),
		toolCall("l", "list_dir"),
		toolCall("w", "write_file"),
		`{"choices":[{"message":{"role":"assistant","content":"stop"}}],"usage":{"total_tokens":1}}`,
	}
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		i := min(n, len(responses)-1)
		n++
		_, _ = w.Write([]byte(responses[i]))
	}))
	defer srv.Close()

	const payload = "package x\n" // the read_file return; its length is what ReadBytes must capture
	reg := tool.NewRegistry()
	reg.Register(tool.Tool{Name: "read_file", Schema: map[string]any{"type": "object"}, Run: func(context.Context, json.RawMessage) (string, error) { return payload, nil }})
	reg.Register(tool.Tool{Name: "list_dir", Schema: map[string]any{"type": "object"}, Run: func(context.Context, json.RawMessage) (string, error) { return "a.go\nb.go", nil }})
	reg.Register(tool.Tool{Name: "write_file", Schema: map[string]any{"type": "object"}, Run: func(context.Context, json.RawMessage) (string, error) { return "wrote 5 bytes", nil }})

	sess := NewSession(llm.NewClient(srv.URL, "m"), reg, llm.Sampling{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{MaxSteps: 10})
	res, err := sess.Run(context.Background(), "sys", "go")
	if err != nil {
		t.Fatal(err)
	}
	if res.Metrics.ToolCalls != 3 {
		t.Errorf("ToolCalls = %d, want 3", res.Metrics.ToolCalls)
	}
	want := map[string]int{"read_file": 1, "list_dir": 1, "write_file": 1}
	for name, c := range want {
		if got := res.Metrics.ToolCounts[name]; got != c {
			t.Errorf("ToolCounts[%q] = %d, want %d", name, got, c)
		}
	}
	if res.Metrics.ReadBytes != len(payload) {
		t.Errorf("ReadBytes = %d, want %d (only the read_file payload, not list_dir/write_file results)", res.Metrics.ReadBytes, len(payload))
	}
}

// TestMetricsAddMergesToolCounts locks the cross-pass aggregation the Ralph loop
// relies on (total.Add(res.Metrics) per pass): per-tool counts sum by name and
// ReadBytes accumulates, starting from a zero-value total whose map is nil — Add
// must allocate it lazily rather than panic.
func TestMetricsAddMergesToolCounts(t *testing.T) {
	var total Metrics // zero value: ToolCounts is nil
	total.Add(Metrics{ToolCalls: 2, ReadBytes: 100, ToolCounts: map[string]int{"read_file": 1, "list_dir": 1}})
	total.Add(Metrics{ToolCalls: 1, ReadBytes: 50, ToolCounts: map[string]int{"read_file": 1, "write_file": 1}})

	if total.ToolCalls != 3 {
		t.Errorf("ToolCalls = %d, want 3", total.ToolCalls)
	}
	if total.ReadBytes != 150 {
		t.Errorf("ReadBytes = %d, want 150", total.ReadBytes)
	}
	want := map[string]int{"read_file": 2, "list_dir": 1, "write_file": 1}
	if len(total.ToolCounts) != len(want) {
		t.Fatalf("ToolCounts = %v, want %v", total.ToolCounts, want)
	}
	for name, c := range want {
		if got := total.ToolCounts[name]; got != c {
			t.Errorf("ToolCounts[%q] = %d, want %d", name, got, c)
		}
	}
}

// TestRunStreamsThroughAgent exercises the streaming path through the loop
// (Config.Stream): call dispatches to CompleteStream, onDelta writes each fragment
// to StreamOut, and the loop ends identically to the non-streaming path. A regression
// that diverged the two paths or dropped the delta wiring would fail here.
func TestRunStreamsThroughAgent(t *testing.T) {
	frames := []string{
		`data: {"choices":[{"delta":{"role":"assistant"}}]}`,
		`data: {"choices":[{"delta":{"content":"hello "}}]}`,
		`data: {"choices":[{"delta":{"content":"world"},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for _, f := range frames {
			_, _ = io.WriteString(w, f+"\n\n")
		}
	}))
	defer srv.Close()

	var out bytes.Buffer
	sess := NewSession(llm.NewClient(srv.URL, "m"), tool.NewRegistry(), llm.Sampling{}, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{MaxSteps: 5, Stream: true, StreamOut: &out})
	res, err := sess.Run(context.Background(), "sys", "go")
	if err != nil {
		t.Fatal(err)
	}
	if res.Reason != "model_stop" {
		t.Fatalf("reason = %q, want model_stop", res.Reason)
	}
	if !strings.Contains(out.String(), "hello world") {
		t.Errorf("streamed output %q missing the streamed content", out.String())
	}
}
