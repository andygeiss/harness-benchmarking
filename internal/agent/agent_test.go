package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
