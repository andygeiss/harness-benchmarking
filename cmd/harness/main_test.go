package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"harness/internal/agent"
	"harness/internal/llm"
	"harness/internal/tool"
)

// discardLog returns a logger that writes nowhere, to keep tests quiet.
func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// scriptedServer stands in for the oMLX endpoint, replying with the given bodies
// in order and repeating the last once they run out.
func scriptedServer(t *testing.T, responses ...string) *httptest.Server {
	t.Helper()
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		i := n
		if i >= len(responses) {
			i = len(responses) - 1
		}
		n++
		_, _ = w.Write([]byte(responses[i]))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newSession(srv *httptest.Server, reg *tool.Registry) *agent.Session {
	return agent.NewSession(llm.NewClient(srv.URL, "test"), reg, llm.Sampling{}, discardLog(), agent.Config{MaxSteps: 5})
}

// stopResponse is an assistant turn with no tool calls: the pass ends as
// model_stop and the workspace is left untouched.
const stopResponse = `{"choices":[{"message":{"role":"assistant","content":"stopping"}}],"usage":{"total_tokens":5}}`

// TestRunCompletes: the model calls done, verification passes, run reports completion.
func TestRunCompletes(t *testing.T) {
	doneCall := `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c","type":"function","function":{"name":"done","arguments":"{\"summary\":\"ok\"}"}}]}}],"usage":{"total_tokens":10}}`
	reg := tool.NewRegistry()
	reg.Register(tool.Done(func(_ context.Context) (bool, string, error) { return true, "", nil }))
	sess := newSession(scriptedServer(t, doneCall), reg)

	if code := run(context.Background(), discardLog(), sess, t.TempDir(), "", "sys", "do it", 3, RunLog{MaxIters: 5}); code != exitCompleted {
		t.Fatalf("exit = %d, want exitCompleted (%d)", code, exitCompleted)
	}
}

// TestRunStagnates: the model never acts, so the workspace is byte-for-byte
// unchanged across passes and the stagnation guard halts the run.
func TestRunStagnates(t *testing.T) {
	sess := newSession(scriptedServer(t, stopResponse), tool.NewRegistry())

	if code := run(context.Background(), discardLog(), sess, t.TempDir(), "", "sys", "go", 2, RunLog{MaxIters: 10}); code != exitStagnated {
		t.Fatalf("exit = %d, want exitStagnated (%d)", code, exitStagnated)
	}
}

// TestRunExhaustsBudget: with the stagnation guard disabled, a model that never
// completes is stopped by the pass budget.
func TestRunExhaustsBudget(t *testing.T) {
	sess := newSession(scriptedServer(t, stopResponse), tool.NewRegistry())

	if code := run(context.Background(), discardLog(), sess, t.TempDir(), "", "sys", "go", 0, RunLog{MaxIters: 3}); code != exitBudget {
		t.Fatalf("exit = %d, want exitBudget (%d)", code, exitBudget)
	}
}

// TestRunWritesLog: a completed run appends one valid RunLog line with the
// outcome and aggregate metrics filled in.
func TestRunWritesLog(t *testing.T) {
	doneCall := `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c","type":"function","function":{"name":"done","arguments":"{\"summary\":\"ok\"}"}}]}}],"usage":{"total_tokens":10}}`
	reg := tool.NewRegistry()
	reg.Register(tool.Done(func(_ context.Context) (bool, string, error) { return true, "", nil }))
	sess := newSession(scriptedServer(t, doneCall), reg)

	logDir := t.TempDir()
	if code := run(context.Background(), discardLog(), sess, t.TempDir(), logDir, "sys", "do it", 3, RunLog{Model: "test", MaxIters: 5}); code != exitCompleted {
		t.Fatalf("exit = %d, want exitCompleted", code)
	}
	data, err := os.ReadFile(filepath.Join(logDir, "runs.jsonl"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	var got RunLog
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("log line is not valid JSON: %v", err)
	}
	if got.Outcome != "completed" || got.Passes != 1 {
		t.Errorf("outcome=%q passes=%d, want completed/1", got.Outcome, got.Passes)
	}
	if got.Model != "test" || got.ModelCalls < 1 || got.ToolCalls < 1 || got.TotalTokens != 10 {
		t.Errorf("metrics not recorded as expected: %+v", got)
	}
}
