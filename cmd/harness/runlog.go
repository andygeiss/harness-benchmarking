package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"harness/internal/agent"
)

// RunLog is one line in <log-dir>/runs.jsonl: the configuration and aggregate
// outcome of a single harness run, written once when the Ralph loop ends. It is
// observability for humans — the agent never sees it.
type RunLog struct {
	Time              string   `json:"time"`
	Model             string   `json:"model"`
	Outcome           string   `json:"outcome"` // completed | stagnated | budget | interrupted | fault
	Passes            int      `json:"passes"`
	PassReasons       []string `json:"pass_reasons"`
	DurationSec       float64  `json:"duration_sec"`
	CtxLimit          int      `json:"ctx_limit"`
	MaxIters          int      `json:"max_iters"`
	MaxSteps          int      `json:"max_steps"`
	MaxTokens         int      `json:"max_tokens"`
	Temperature       float64  `json:"temperature"`
	TopP              float64  `json:"top_p"`
	TopK              int      `json:"top_k"`
	MinP              float64  `json:"min_p"`
	RepetitionPenalty float64  `json:"repetition_penalty"`
	PresencePenalty   float64  `json:"presence_penalty"`
	agent.Metrics              // model_calls, tool_calls, token counts, server_time_sec — flattened
}

// appendRunLog appends rec as one JSON line to <dir>/runs.jsonl, creating dir as
// needed. Logging is best-effort: the caller logs a returned error but never
// fails the run over it.
func appendRunLog(dir string, rec RunLog) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "runs.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(append(line, '\n'))
	return err
}
