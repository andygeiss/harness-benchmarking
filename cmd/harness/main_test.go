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
	"strings"
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

	if code := run(context.Background(), discardLog(), sess, t.TempDir(), "", "sys", "do it", 3, true, nil, RunLog{MaxIters: 5}); code != exitCompleted {
		t.Fatalf("exit = %d, want exitCompleted (%d)", code, exitCompleted)
	}
}

// TestRunStagnates: the model never acts, so the workspace is byte-for-byte
// unchanged across passes and the stagnation guard halts the run.
func TestRunStagnates(t *testing.T) {
	sess := newSession(scriptedServer(t, stopResponse), tool.NewRegistry())

	if code := run(context.Background(), discardLog(), sess, t.TempDir(), "", "sys", "go", 2, true, nil, RunLog{MaxIters: 10}); code != exitStagnated {
		t.Fatalf("exit = %d, want exitStagnated (%d)", code, exitStagnated)
	}
}

// TestRunExhaustsBudget: with the stagnation guard disabled, a model that never
// completes is stopped by the pass budget.
func TestRunExhaustsBudget(t *testing.T) {
	sess := newSession(scriptedServer(t, stopResponse), tool.NewRegistry())

	if code := run(context.Background(), discardLog(), sess, t.TempDir(), "", "sys", "go", 0, true, nil, RunLog{MaxIters: 3}); code != exitBudget {
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
	if code := run(context.Background(), discardLog(), sess, t.TempDir(), logDir, "sys", "do it", 3, true, nil, RunLog{Model: "test", MaxIters: 5}); code != exitCompleted {
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

// TestRunCompletesViaProbe: the model changes the workspace (writes a file) but
// stops without calling done. The outer loop's end-of-pass probe runs the
// verifier on the already-changed workspace, finds it green, and completes the
// run in that same pass — no extra pass spent only to call the gate. The log
// shows the giveaway: a single pass that ended model_stop, yet outcome=completed.
func TestRunCompletesViaProbe(t *testing.T) {
	writeCall := `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"w","type":"function","function":{"name":"write_file","arguments":"{\"path\":\"impl.go\",\"content\":\"package p\\n\"}"}}]}}],"usage":{"total_tokens":10}}`
	work := t.TempDir()
	reg := tool.NewRegistry()
	reg.Register(tool.WriteFile(work, false))
	sess := newSession(scriptedServer(t, writeCall, stopResponse), reg)

	var probed bool
	verify := func(_ context.Context) (bool, string, error) {
		probed = true
		return true, "", nil // the changed workspace passes verification
	}

	logDir := t.TempDir()
	code := run(context.Background(), discardLog(), sess, work, logDir, "sys", "go", 3, true, verify, RunLog{MaxIters: 5})
	if code != exitCompleted {
		t.Fatalf("exit = %d, want exitCompleted (%d)", code, exitCompleted)
	}
	if !probed {
		t.Fatal("the end-of-pass probe never ran the verifier")
	}
	data, err := os.ReadFile(filepath.Join(logDir, "runs.jsonl"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	var got RunLog
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("log not valid JSON: %v", err)
	}
	if got.Outcome != "completed" || got.Passes != 1 {
		t.Errorf("outcome=%q passes=%d, want completed/1", got.Outcome, got.Passes)
	}
	if len(got.PassReasons) != 1 || got.PassReasons[0] != "model_stop" {
		t.Errorf("pass_reasons = %v, want [model_stop] (completion via the probe, not done)", got.PassReasons)
	}
}

// TestSystemPromptMemoryToggle: the built-in prompt mentions PROGRESS.md only
// when memory is on, and otherwise shares the same head, so the two modes differ
// by exactly the memory guidance and cannot silently drift apart.
func TestSystemPromptMemoryToggle(t *testing.T) {
	on, off := systemPrompt(true), systemPrompt(false)
	if !strings.Contains(on, "PROGRESS.md") {
		t.Error("memory-on prompt should mention PROGRESS.md")
	}
	if strings.Contains(off, "PROGRESS.md") {
		t.Error("memory-off prompt must not mention PROGRESS.md")
	}
	if !strings.Contains(on, systemHead) || !strings.Contains(off, systemHead) {
		t.Error("both prompts must share the common head")
	}
}

// TestWipeScratch removes the scratch files and leaves everything else; a second
// call is a no-op rather than an error.
func TestWipeScratch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "PROGRESS.md"), []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keep.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := wipeScratch(dir); err != nil {
		t.Fatalf("wipeScratch: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "PROGRESS.md")); !os.IsNotExist(err) {
		t.Error("PROGRESS.md should have been removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "keep.go")); err != nil {
		t.Errorf("keep.go should remain: %v", err)
	}
	if err := wipeScratch(dir); err != nil {
		t.Fatalf("second wipeScratch (nothing to remove): %v", err)
	}
}

// TestRunWipesProgressWhenMemoryOff: with memory off the loop removes PROGRESS.md
// before each pass, so a note in the workspace does not survive; with memory on it
// is left untouched. The model does nothing (stopResponse), so the run ends via
// the stagnation guard either way — only PROGRESS.md's fate differs.
func TestRunWipesProgressWhenMemoryOff(t *testing.T) {
	for _, tc := range []struct {
		name   string
		memory bool
		gone   bool
	}{
		{"memory off wipes", false, true},
		{"memory on keeps", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			work := t.TempDir()
			progress := filepath.Join(work, "PROGRESS.md")
			if err := os.WriteFile(progress, []byte("done: nothing"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(work, "keep.go"), []byte("package p\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			sess := newSession(scriptedServer(t, stopResponse), tool.NewRegistry())
			run(context.Background(), discardLog(), sess, work, "", "sys", "go", 2, tc.memory, nil, RunLog{MaxIters: 10})

			_, err := os.Stat(progress)
			if tc.gone && !os.IsNotExist(err) {
				t.Error("PROGRESS.md should have been wiped with memory off")
			}
			if !tc.gone && err != nil {
				t.Errorf("PROGRESS.md should remain with memory on: %v", err)
			}
		})
	}
}
