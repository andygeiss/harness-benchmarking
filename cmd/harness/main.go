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

// The system prompt is assembled from three parts so the cross-pass memory
// guidance can be toggled out by -memory=false (see systemPrompt). The memory
// bullet is the only difference between the two variants; everything else is
// shared, so the prompt cannot drift between modes.
const systemHead = `You are a senior Go engineer working autonomously in a Go workspace.
You act by calling tools, observe the results, and continue until the task is verifiably complete.

Workspace and tools:
- All paths are relative to the workspace root; you cannot access files outside it.
- read_file, write_file, edit_file, list_dir inspect and modify files. edit_file replaces an exact, unique snippet and is preferred for small changes; write_file replaces the WHOLE file (include the complete contents). Read a file before editing or overwriting it.
- go runs the Go toolchain: ["build","./..."], ["test","./..."], ["vet","./..."], ["fmt","./..."], ["mod","tidy"]. There is no shell, linter, or go doc; to learn an API, read its source.
- done signals completion. The harness then runs verification; if it passes the run ends, otherwise you receive the errors and must fix them and call done again.

How to work:
- Make the smallest change that satisfies the task: edit existing code rather than rewriting a whole file, and touch only the files the task requires.
- Work in small, verified increments. After changing code, run go build ./..., go vet ./..., then go test ./... (add -race for concurrent code) and fix every failure before continuing.
- Do not invent APIs: call only functions, fields, and packages you have seen in this workspace or the standard library — hallucinated identifiers are the most common failure here. If unsure one exists, read its source first.
- If the same failure persists after a few attempts, change your approach; re-running an identical edit or command will not change the result.`

// memoryGuidance tells the agent it has cross-pass memory via PROGRESS.md. It is
// included only when -memory is on; -memory=false drops it and wipes PROGRESS.md
// before each pass, so a run measures how well the model resumes from the
// persisted code alone (see wipeScratch and the Ralph loop in run).
const memoryGuidance = `
- Your context is reset between passes. Keep a short PROGRESS.md at the workspace root recording what is done, what remains, and key decisions. Read it first each pass and keep it current — it is your memory across resets.`

const systemTail = `
- Use only the Go standard library unless the task explicitly requires otherwise. Keep changes minimal and idiomatic, targeting Go 1.26.
- Idioms that matter: wrap errors with %w and branch with errors.Is/errors.As, not string matching; take context.Context as the first parameter and honour cancellation so no goroutine leaks.
- Be terse and reason proportionally to the task — your context window is the binding budget; spend it on code, not deliberation.
- Call done only when go build and go test pass; never signal completion over a red build or a failing test.`

// systemPrompt returns the built-in system prompt, including the PROGRESS.md
// memory guidance only when memory is true.
func systemPrompt(memory bool) string {
	if memory {
		return systemHead + memoryGuidance + systemTail
	}
	return systemHead + systemTail
}

// Process exit codes. 2 is intentionally unused: the flag package and the
// missing-prompt check already use it for usage errors, its conventional
// meaning. The non-zero loop outcomes are kept distinct so a caller can tell a
// stuck model (exitStagnated) from one that simply ran out of passes (exitBudget).
const (
	exitCompleted = 0 // task verified complete, or the run was cleanly interrupted
	exitFault     = 1 // a setup step failed, an unexpected error occurred, or every pass errored (e.g. endpoint unreachable)
	exitStagnated = 3 // the workspace went unchanged across passes that ran cleanly; the model is stuck
	exitBudget    = 4 // the Ralph pass budget ran out before completion
)

func main() {
	endpoint := flag.String("endpoint", "http://localhost:1234/v1", "base URL of the OpenAI-compatible oMLX server")
	model := flag.String("model", "Qwen3.6-35B-A3B-oQ6-mtp", "model name")
	promptPath := flag.String("prompt", "", "path to the task prompt file (required)")
	systemPath := flag.String("system", "", "path to a system prompt file (optional; overrides the built-in)")
	workdir := flag.String("workdir", ".", "workspace directory the agent operates in")
	verifyCmd := flag.String("verify", "go test ./...", "verification command run by the done gate (split on whitespace; cannot carry quoted or spaced arguments)")
	maxIters := flag.Int("max-iters", 25, "maximum Ralph passes")
	maxSteps := flag.Int("max-steps", 40, "maximum tool steps per pass")
	ctxLimit := flag.Int("ctx-limit", 32768, "end a pass once total tokens reach this")
	maxStale := flag.Int("max-stale", 3, "stop after this many consecutive passes leave the workspace unchanged (0 disables)")
	logDir := flag.String("log-dir", "logs", "directory for the JSONL run log, relative to the working dir (empty disables)")
	protectTests := flag.Bool("protect-tests", true, "refuse agent writes to *_test.go files — the tests are the fixed spec, not the model's to author")
	cmdTimeout := flag.Duration("cmd-timeout", 5*time.Minute, "timeout per go/verify command")
	stream := flag.Bool("stream", false, "stream tokens live to stderr as the model generates")
	debug := flag.Bool("debug", false, "log model reasoning and verbose detail")
	memory := flag.Bool("memory", true, "carry PROGRESS.md across passes as the agent's plan memory; -memory=false ablates it (drops the PROGRESS.md guidance from the built-in prompt and wipes PROGRESS.md before each pass) to measure resumption from the persisted code alone")
	elidePassing := flag.Bool("elide-passing", true, "stub a *_test.go spec on read once its package's tests pass, so a fresh pass does not re-spend context re-reading verified specs (go-test verify only); on by default — -elide-passing=false ablates it for the stagnation A/B (see docs/stagnation.md)")

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
	system := systemPrompt(*memory)
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

	// read_file stubs the specs of packages the verifier has certified green, so a
	// fresh pass does not re-spend its budget re-reading satisfied specs (the
	// re-orientation floor; see docs/stagnation.md). The set is fed by the verifier
	// below — which runs anyway for the done gate and the end-of-pass probe — so no
	// extra test run is needed; a non-go-test verify command never feeds it, leaving
	// reads unchanged. Elision is on by default; -elide-passing=false leaves the state
	// nil — the OFF arm that restores the pre-elision baseline so the floor it clears
	// can be measured on a second model (Part 7 anticipated this toggle). ReadFile,
	// Update, and Elided are all nil-safe, so the onPass wiring below is unchanged.
	var elide *tool.ElideState
	if *elidePassing {
		elide = tool.NewElideState()
	}

	reg := tool.NewRegistry()
	reg.Register(tool.ReadFile(absWork, elide))
	reg.Register(tool.WriteFile(absWork, *protectTests))
	reg.Register(tool.EditFile(absWork, *protectTests))
	reg.Register(tool.ListDir(absWork))
	reg.Register(tool.Go(absWork, *cmdTimeout))
	verifier := tool.VerifierFor(absWork, strings.Fields(*verifyCmd), *cmdTimeout, elide.Update)
	reg.Register(tool.Done(verifier))

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
		Task:              *promptPath,
		Memory:            *memory,
		ElidePassing:      *elidePassing,
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
	log.Info("starting", "model", *model, "workdir", absWork, "verify", *verifyCmd, "max_iters", *maxIters, "memory", *memory, "elide_passing", *elidePassing)
	os.Exit(run(ctx, log, sess, absWork, *logDir, system, prompt, *maxStale, *memory, verifier, elide, rec))
}

// run drives the Ralph loop: it re-runs the session with a fresh context each
// pass until the task verifies complete, the workspace stagnates, or the pass
// budget is exhausted, and returns the process exit code. A run in which every
// pass errored — none ever ran cleanly — returns exitFault rather than
// exitStagnated or exitBudget, so an unreachable endpoint is not misread as a
// stuck model. verify is the done
// gate's Verifier, reused by the end-of-pass probe (see the loop body). os.Exit
// stays in main, so the loop's control logic is exercised directly by tests.
// When logDir is set it appends one RunLog record (config, outcome, aggregate
// metrics) on exit. When memory is false the agent's plan memory is ablated:
// PROGRESS.md is wiped before each pass, so the run measures how well the model
// resumes from the persisted code alone rather than from its own notes.
func run(ctx context.Context, log *slog.Logger, sess *agent.Session, workdir, logDir, system, prompt string, maxStale int, memory bool, verify tool.Verifier, elide *tool.ElideState, rec RunLog) int {
	start := time.Now()
	rec.Time = start.Format(time.RFC3339)
	rec.Outcome = "fault"
	var total agent.Metrics
	if logDir != "" {
		defer func() {
			rec.DurationSec = time.Since(start).Seconds()
			rec.Metrics = total
			rec.ElidedReads = elide.Elided()
			if err := appendRunLog(logDir, rec); err != nil {
				log.Warn("write run log", "err", err)
			}
		}()
	}

	// lastVerified is the workspace state the end-of-pass probe last ran the
	// verifier against; it starts at the seed so the probe fires only once the
	// model has actually changed something.
	lastVerified, _ := fingerprint(workdir)
	stale := staleTracker{limit: maxStale}
	// anyClean records whether at least one pass ran to a clean (non-error)
	// result. If none ever does — every pass errored, e.g. the endpoint was
	// unreachable — the run is a fault, not a stuck model or an exhausted budget.
	anyClean := false
	for iter := 1; iter <= rec.MaxIters; iter++ {
		// Memory ablation: with -memory=false, remove PROGRESS.md before the
		// pass so the model cannot carry plan notes across the context reset.
		// scratchFiles are excluded from the fingerprint, so wiping them never
		// perturbs the stagnation guard.
		if !memory {
			if err := wipeScratch(workdir); err != nil {
				log.Warn("wipe scratch files", "err", err)
			}
		}
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
			anyClean = true
			log.Info("pass ended", "iter", iter, "reason", res.Reason, "steps", res.Steps)
			rec.PassReasons = append(rec.PassReasons, res.Reason)
			if res.Completed {
				log.Info("task complete and verified", "passes", iter)
				rec.Outcome = "completed"
				return exitCompleted
			}
		}

		// Fingerprint the workspace once; the end-of-pass probe and the
		// stagnation guard both key off whether this pass changed anything.
		fp, ferr := fingerprint(workdir)
		if ferr != nil {
			log.Warn("fingerprint workspace", "err", ferr)
		}

		// End-of-pass verification probe: when the pass changed the workspace
		// but did not end in a successful done — the model stopped, hit a budget,
		// or even errored after the work was already correct — run the same gate
		// here and finish now if it passes, instead of spending another pass
		// only to re-verify already-correct code (see probeComplete).
		done, lv := probeComplete(ctx, log, verify, fp, lastVerified, iter)
		lastVerified = lv
		if done {
			log.Info("task complete and verified", "passes", iter, "via", "end-of-pass probe")
			rec.Outcome = "completed"
			return exitCompleted
		}

		// Stagnation guard: consecutive passes that leave the workspace
		// byte-for-byte unchanged mean the model is stuck — a fresh context will
		// only reproduce the same non-result, so stop instead of burning the
		// remaining budget.
		if outcome, code, stop := checkStagnation(log, &stale, fp, ferr, anyClean, iter); stop {
			rec.Outcome = outcome
			return code
		}
	}
	if !anyClean {
		log.Error("every pass errored before the budget ran out; treating as a fault", "max", rec.MaxIters)
		rec.Outcome = "fault"
		return exitFault
	}
	log.Warn("reached max Ralph passes without completion", "max", rec.MaxIters)
	rec.Outcome = "budget"
	return exitBudget
}

// checkStagnation folds this pass's fingerprint into the stale tracker and decides
// whether the run must stop. A failed fingerprint (ferr != nil) skips the check — a
// fresh context will only reproduce the same non-result. If the workspace has gone
// unchanged for the tracker's limit, the run stops: a fault when no pass ever ran
// clean (every pass errored — an unreachable model, not a stuck one), otherwise
// stagnated. stop=false means continue. Kept out of run to hold its complexity down.
func checkStagnation(log *slog.Logger, stale *staleTracker, fp string, ferr error, anyClean bool, iter int) (outcome string, code int, stop bool) {
	if ferr != nil {
		return "", 0, false
	}
	stalled := stale.update(fp)
	log.Debug("workspace fingerprint", "iter", iter, "stale", stale.count, "hash", fp)
	if !stalled {
		return "", 0, false
	}
	if !anyClean {
		log.Error("every pass errored and the workspace never changed; treating as a fault", "passes", stale.count, "iter", iter)
		return "fault", exitFault, true
	}
	log.Warn("workspace unchanged across consecutive passes; stopping", "passes", stale.count, "iter", iter)
	return "stagnated", exitStagnated, true
}

// probeComplete is the outer-loop counterpart to the model calling done: when a
// pass has advanced the workspace (fp != lastVerified) it runs the SAME Verifier
// the done gate uses, so a pass that finished the work but did not signal
// completion — the model stopped, hit a budget, or errored after the code was
// already correct — is recognised here instead of wasting another pass. It
// returns whether the task now verifies and the fingerprint to remember as last
// verified — advanced to fp once the gate gives a definitive answer (so identical
// bytes are not re-verified), left unchanged on a transient verifier error so the
// next pass may retry. A nil verify, a failed fingerprint (fp==""), or an
// unchanged workspace is a no-op.
func probeComplete(ctx context.Context, log *slog.Logger, verify tool.Verifier, fp, lastVerified string, iter int) (completed bool, verified string) {
	if verify == nil || fp == "" || fp == lastVerified {
		return false, lastVerified
	}
	ok, _, err := verify(ctx)
	if err != nil {
		log.Warn("end-of-pass verify probe could not run", "iter", iter, "err", err)
		return false, lastVerified
	}
	return ok, fp
}
