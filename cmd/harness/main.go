// Command harness runs a fixed prompt autonomously against a local oMLX-served
// Qwen3.6 model, looping until the task verifies complete — a Go "Ralph" loop.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"harness/internal/agent"
	"harness/internal/llm"
	"harness/internal/tool"
)

const defaultSystem = `You are an autonomous software engineer working in a Go workspace.
You act by calling tools, observe the results, and continue until the task is complete.

Workspace and tools:
- All paths are relative to the workspace root; you cannot access files outside it.
- read_file, write_file, edit_file, list_dir inspect and modify files. edit_file replaces an exact, unique snippet and is preferred for small changes; write_file replaces the WHOLE file (include the complete contents). Read a file before editing or overwriting it.
- go runs the Go toolchain: ["build","./..."], ["test","./..."], ["vet","./..."], ["fmt","./..."], ["mod","tidy"].
- done signals completion. The harness then runs verification; if it passes the run ends, otherwise you receive the errors and must fix them and call done again.

How to work:
- Work in small, verified increments. After changing code, run go build ./... then go test ./... and fix failures before continuing.
- Your context is reset between passes. Keep a short PROGRESS.md at the workspace root recording what is done, what remains, and key decisions. Read it first each pass and keep it current — it is your memory across resets.
- Use only the Go standard library unless the task explicitly requires otherwise. Keep changes minimal and idiomatic.
- Do not call done until the implementation is complete and you expect verification to pass.`

// Process exit codes. 2 is intentionally unused: the flag package and the
// missing-prompt check already use it for usage errors, its conventional
// meaning. The non-zero loop outcomes are kept distinct so a caller can tell a
// stuck model (exitStagnated) from one that simply ran out of passes (exitBudget).
const (
	exitCompleted = 0 // task verified complete, or the run was cleanly interrupted
	exitFault     = 1 // a setup step failed, or an unexpected error occurred
	exitStagnated = 3 // the workspace went unchanged across passes; the model is stuck
	exitBudget    = 4 // the Ralph pass budget ran out before completion
)

func main() {
	endpoint := flag.String("endpoint", "http://localhost:1234/v1", "base URL of the OpenAI-compatible oMLX server")
	model := flag.String("model", "Qwen3.6-35B-A3B-oQ6-fp16-mtp", "model name")
	promptPath := flag.String("prompt", "", "path to the task prompt file (required)")
	systemPath := flag.String("system", "", "path to a system prompt file (optional; overrides the built-in)")
	workdir := flag.String("workdir", ".", "workspace directory the agent operates in")
	verifyCmd := flag.String("verify", "go test ./...", "verification command run by the done gate")
	maxIters := flag.Int("max-iters", 25, "maximum Ralph passes")
	maxSteps := flag.Int("max-steps", 40, "maximum tool steps per pass")
	ctxLimit := flag.Int("ctx-limit", 52000, "end a pass once total tokens reach this")
	maxStale := flag.Int("max-stale", 3, "stop after this many consecutive passes leave the workspace unchanged (0 disables)")
	logDir := flag.String("log-dir", "logs", "directory for the JSONL run log, relative to the working dir (empty disables)")
	protectTests := flag.Bool("protect-tests", true, "refuse agent writes to *_test.go files — the tests are the fixed spec, not the model's to author")
	cmdTimeout := flag.Duration("cmd-timeout", 5*time.Minute, "timeout per go/verify command")
	stream := flag.Bool("stream", false, "stream tokens live to stderr as the model generates")
	debug := flag.Bool("debug", false, "log model reasoning and verbose detail")

	maxTokens := flag.Int("max-tokens", 32768, "max output tokens per call")
	temp := flag.Float64("temp", 0.6, "temperature")
	topP := flag.Float64("top-p", 0.95, "top_p")
	topK := flag.Int("top-k", 20, "top_k")
	minP := flag.Float64("min-p", 0, "min_p")
	repPenalty := flag.Float64("rep-penalty", 1.0, "repetition_penalty")
	presencePenalty := flag.Float64("presence-penalty", 0, "presence_penalty")
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	if *promptPath == "" {
		log.Error("the -prompt flag is required")
		flag.Usage()
		os.Exit(2)
	}
	promptBytes, err := os.ReadFile(*promptPath)
	if err != nil {
		log.Error("read prompt", "err", err)
		os.Exit(exitFault)
	}
	system := defaultSystem
	if *systemPath != "" {
		b, err := os.ReadFile(*systemPath)
		if err != nil {
			log.Error("read system prompt", "err", err)
			os.Exit(exitFault)
		}
		system = string(b)
	}
	absWork, err := filepath.Abs(*workdir)
	if err != nil {
		log.Error("resolve workdir", "err", err)
		os.Exit(exitFault)
	}

	reg := tool.NewRegistry()
	reg.Register(tool.ReadFile(absWork))
	reg.Register(tool.WriteFile(absWork, *protectTests))
	reg.Register(tool.EditFile(absWork, *protectTests))
	reg.Register(tool.ListDir(absWork))
	reg.Register(tool.Go(absWork, *cmdTimeout))
	reg.Register(tool.Done(tool.CommandVerifier(absWork, strings.Fields(*verifyCmd), *cmdTimeout)))

	client := llm.NewClient(*endpoint, *model)
	sess := agent.NewSession(client, reg, llm.Sampling{
		MaxTokens:         *maxTokens,
		Temperature:       *temp,
		TopP:              *topP,
		TopK:              *topK,
		MinP:              *minP,
		RepetitionPenalty: *repPenalty,
		PresencePenalty:   *presencePenalty,
	}, log, agent.Config{
		MaxSteps:  *maxSteps,
		CtxLimit:  *ctxLimit,
		Stream:    *stream,
		StreamOut: os.Stderr,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	prompt := string(promptBytes)
	rec := RunLog{
		Model:             *model,
		CtxLimit:          *ctxLimit,
		MaxIters:          *maxIters,
		MaxSteps:          *maxSteps,
		MaxTokens:         *maxTokens,
		Temperature:       *temp,
		TopP:              *topP,
		TopK:              *topK,
		MinP:              *minP,
		RepetitionPenalty: *repPenalty,
		PresencePenalty:   *presencePenalty,
	}
	log.Info("starting", "model", *model, "workdir", absWork, "verify", *verifyCmd, "max_iters", *maxIters)
	os.Exit(run(ctx, log, sess, absWork, *logDir, system, prompt, *maxStale, rec))
}

// run drives the Ralph loop: it re-runs the session with a fresh context each
// pass until the task verifies complete, the workspace stagnates, or the pass
// budget is exhausted, and returns the process exit code. os.Exit stays in main,
// so the loop's control logic is exercised directly by tests. When logDir is set
// it appends one RunLog record (config, outcome, aggregate metrics) on exit.
func run(ctx context.Context, log *slog.Logger, sess *agent.Session, workdir, logDir, system, prompt string, maxStale int, rec RunLog) int {
	start := time.Now()
	rec.Time = start.Format(time.RFC3339)
	rec.Outcome = "fault"
	var total agent.Metrics
	if logDir != "" {
		defer func() {
			rec.DurationSec = time.Since(start).Seconds()
			rec.Metrics = total
			if err := appendRunLog(logDir, rec); err != nil {
				log.Warn("write run log", "err", err)
			}
		}()
	}

	var prevFP string
	var stale int
	for iter := 1; iter <= rec.MaxIters; iter++ {
		log.Info("ralph pass", "iter", iter, "max", rec.MaxIters)
		res, err := sess.Run(ctx, system, prompt)
		total.Add(res.Metrics)
		rec.Passes = iter
		if ctx.Err() != nil {
			log.Info("interrupted")
			rec.Outcome = "interrupted"
			return exitCompleted
		}
		if err != nil {
			log.Error("pass failed", "iter", iter, "err", err)
			rec.PassReasons = append(rec.PassReasons, "error")
		} else {
			log.Info("pass ended", "iter", iter, "reason", res.Reason, "steps", res.Steps)
			rec.PassReasons = append(rec.PassReasons, res.Reason)
			if res.Completed {
				log.Info("task complete and verified", "passes", iter)
				rec.Outcome = "completed"
				return exitCompleted
			}
		}

		// Stagnation guard: if consecutive passes leave the workspace
		// byte-for-byte unchanged, the model is stuck — a fresh context will
		// only reproduce the same non-result, so stop instead of burning the
		// remaining budget. A failed fingerprint is non-fatal: skip the check.
		if maxStale > 0 {
			fp, ferr := fingerprint(workdir)
			if ferr != nil {
				log.Warn("fingerprint workspace", "err", ferr)
			} else {
				if prevFP != "" && fp == prevFP {
					stale++
				} else {
					stale = 0
				}
				prevFP = fp
				log.Debug("workspace fingerprint", "iter", iter, "stale", stale, "hash", fp)
				if stale >= maxStale {
					log.Warn("workspace unchanged across consecutive passes; stopping", "passes", stale, "iter", iter)
					rec.Outcome = "stagnated"
					return exitStagnated
				}
			}
		}
	}
	log.Warn("reached max Ralph passes without completion", "max", rec.MaxIters)
	rec.Outcome = "budget"
	return exitBudget
}
