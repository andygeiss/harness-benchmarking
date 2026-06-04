// Package agent runs a single autonomous session: the model takes actions via
// tools until it stops, completes the task, or hits a budget.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"harness/internal/llm"
	"harness/internal/tool"
)

// Config holds the per-session knobs.
type Config struct {
	MaxSteps  int       // hard cap on tool steps per session
	CtxLimit  int       // end the session once total tokens reach this (0 disables)
	Stream    bool      // stream tokens live via the model's SSE interface
	StreamOut io.Writer // where streamed tokens are written (defaults to io.Discard)
}

// Session drives one inner loop against the model. It holds no per-run state, so
// the same Session is reused across Ralph passes — each pass starts fresh.
type Session struct {
	client   *llm.Client
	registry *tool.Registry
	sampling llm.Sampling
	log      *slog.Logger
	cfg      Config
}

// NewSession wires a Session.
func NewSession(client *llm.Client, registry *tool.Registry, sampling llm.Sampling, log *slog.Logger, cfg Config) *Session {
	if cfg.StreamOut == nil {
		cfg.StreamOut = io.Discard
	}
	return &Session{client: client, registry: registry, sampling: sampling, log: log, cfg: cfg}
}

// Result reports how a session ended.
type Result struct {
	Completed bool   // task verified complete via the done gate
	Reason    string // completed | model_stop | context | max_steps | empty
	Steps     int
}

// Run executes one session from a fresh context built from system and prompt.
func (s *Session) Run(ctx context.Context, system, prompt string) (Result, error) {
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: system},
		{Role: llm.RoleUser, Content: prompt},
	}
	specs := s.registry.Specs()

	for step := 1; step <= s.cfg.MaxSteps; step++ {
		resp, err := s.complete(ctx, s.request(msgs, specs))
		if err != nil {
			return Result{}, err
		}
		if len(resp.Choices) == 0 {
			return Result{Reason: "empty", Steps: step}, nil
		}

		choice := resp.Choices[0]
		content, reasoning := llm.SplitReasoning(choice.Message)
		if reasoning != "" {
			s.log.Debug("reasoning", "step", step, "text", reasoning)
		}

		// Append the assistant turn WITHOUT the reasoning trace.
		msgs = append(msgs, llm.Message{
			Role:      llm.RoleAssistant,
			Content:   content,
			ToolCalls: choice.Message.ToolCalls,
		})

		if len(choice.Message.ToolCalls) == 0 {
			s.log.Info("model stopped", "step", step, "finish", choice.FinishReason)
			return Result{Reason: "model_stop", Steps: step}, nil
		}

		for _, tc := range choice.Message.ToolCalls {
			result, completed := s.runTool(ctx, tc)
			msgs = append(msgs, llm.Message{Role: llm.RoleTool, ToolCallID: tc.ID, Content: result})
			if completed {
				return Result{Completed: true, Reason: "completed", Steps: step}, nil
			}
		}

		if s.cfg.CtxLimit > 0 && resp.Usage.TotalTokens >= s.cfg.CtxLimit {
			s.log.Info("context budget reached; ending pass", "step", step, "tokens", resp.Usage.TotalTokens)
			return Result{Reason: "context", Steps: step}, nil
		}
	}
	return Result{Reason: "max_steps", Steps: s.cfg.MaxSteps}, nil
}

// maxRetries is how many times complete retries a transient LLM failure.
const maxRetries = 2

// complete calls the model, retrying transient failures (transport errors,
// truncated reads, 5xx) with backoff. Permanent errors and a cancelled context
// return immediately.
func (s *Session) complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	for attempt := 0; ; attempt++ {
		resp, err := s.call(ctx, req)
		if err == nil || !llm.Retryable(err) || ctx.Err() != nil || attempt >= maxRetries {
			return resp, err
		}
		wait := backoff(attempt)
		s.log.Warn("transient LLM error; retrying", "attempt", attempt+1, "in", wait, "err", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}

// backoff returns the delay before retry attempt n (0-based): 200ms, 400ms, ...
func backoff(n int) time.Duration {
	return time.Duration(200<<n) * time.Millisecond
}

// call dispatches to the streaming or non-streaming client call.
func (s *Session) call(ctx context.Context, req llm.Request) (*llm.Response, error) {
	if !s.cfg.Stream {
		return s.client.Complete(ctx, req)
	}
	resp, err := s.client.CompleteStream(ctx, req, s.onDelta)
	if err == nil {
		fmt.Fprintln(s.cfg.StreamOut) // terminate the streamed line
	}
	return resp, err
}

func (s *Session) onDelta(_, text string) {
	fmt.Fprint(s.cfg.StreamOut, text)
}

func (s *Session) request(msgs []llm.Message, specs []llm.Tool) llm.Request {
	return llm.Request{
		Messages:          msgs,
		Tools:             specs,
		Stream:            false,
		MaxTokens:         s.sampling.MaxTokens,
		Temperature:       s.sampling.Temperature,
		TopP:              s.sampling.TopP,
		TopK:              s.sampling.TopK,
		MinP:              s.sampling.MinP,
		RepetitionPenalty: s.sampling.RepetitionPenalty,
		PresencePenalty:   s.sampling.PresencePenalty,
	}
}

// runTool executes one tool call, returning the result to feed back and whether
// it signalled successful completion.
func (s *Session) runTool(ctx context.Context, tc llm.ToolCall) (result string, completed bool) {
	t, ok := s.registry.Get(tc.Function.Name)
	if !ok {
		s.log.Warn("unknown tool", "name", tc.Function.Name)
		return fmt.Sprintf("ERROR: unknown tool %q. Available tools: %s. Call a tool through the function-calling API with a valid name; do not put the call in inline text.",
			tc.Function.Name, strings.Join(s.registry.Names(), ", ")), false
	}
	s.log.Info("tool", "name", tc.Function.Name, "args", tc.Function.Arguments)

	out, err := t.Run(ctx, json.RawMessage(tc.Function.Arguments))
	if errors.Is(err, tool.ErrCompleted) {
		return "", true
	}
	if err != nil {
		s.log.Warn("tool error", "name", tc.Function.Name, "err", err)
		return "ERROR: " + err.Error(), false
	}
	return out, false
}
