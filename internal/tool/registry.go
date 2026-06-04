// Package tool defines the agent's tools and a registry that exposes them to
// the model as native function-calling specs.
package tool

import (
	"context"
	"encoding/json"

	"harness/internal/llm"
)

// Handler executes a tool call. args is the raw JSON object the model supplied;
// the returned string is fed back to the model as the tool result.
type Handler func(ctx context.Context, args json.RawMessage) (string, error)

// Tool is a single callable capability.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any // JSON Schema for the arguments
	Run         Handler
}

// Registry holds tools in registration order.
type Registry struct {
	tools map[string]Tool
	order []string
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool)}
}

// Register adds a tool; re-registering a name replaces the prior definition.
func (r *Registry) Register(t Tool) {
	if _, exists := r.tools[t.Name]; !exists {
		r.order = append(r.order, t.Name)
	}
	r.tools[t.Name] = t
}

// Get returns the named tool.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Names returns the registered tool names in registration order.
func (r *Registry) Names() []string {
	names := make([]string, len(r.order))
	copy(names, r.order)
	return names
}

// Specs returns the tools as native function-calling definitions, in order.
func (r *Registry) Specs() []llm.Tool {
	specs := make([]llm.Tool, 0, len(r.order))
	for _, name := range r.order {
		t := r.tools[name]
		specs = append(specs, llm.Tool{
			Type: "function",
			Function: llm.ToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Schema,
			},
		})
	}
	return specs
}
