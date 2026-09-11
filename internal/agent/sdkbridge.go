package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/codeany-ai/open-agent-sdk-go/costtracker"
	sdktools "github.com/codeany-ai/open-agent-sdk-go/tools"
	sdktypes "github.com/codeany-ai/open-agent-sdk-go/types"

	"github.com/fastclaw-ai/fastclaw/internal/agent/tools"
	"github.com/fastclaw-ai/fastclaw/internal/provider"
)

// readOnlyTools lists tools that are safe to run concurrently.
var readOnlyTools = map[string]bool{
	"read_file":     true,
	"list_dir":      true,
	"web_fetch":     true,
	"web_search":    true,
	"memory_search": true,
	"load_skill":    true,
}

// toolAdapter wraps a FastClaw tool as an SDK Tool interface.
type toolAdapter struct {
	name        string
	description string
	params      interface{}
	fn          tools.ToolFunc
}

func (t *toolAdapter) Name() string        { return t.name }
func (t *toolAdapter) Description() string { return t.description }

func (t *toolAdapter) InputSchema() sdktypes.ToolInputSchema {
	// Convert FastClaw params (interface{}) to SDK ToolInputSchema
	if t.params == nil {
		return sdktypes.ToolInputSchema{Type: "object"}
	}
	data, err := json.Marshal(t.params)
	if err != nil {
		return sdktypes.ToolInputSchema{Type: "object"}
	}
	var schema sdktypes.ToolInputSchema
	if err := json.Unmarshal(data, &schema); err != nil {
		return sdktypes.ToolInputSchema{Type: "object"}
	}
	return schema
}

func (t *toolAdapter) Call(ctx context.Context, input map[string]interface{}, tCtx *sdktypes.ToolUseContext) (*sdktypes.ToolResult, error) {
	argsJSON, err := json.Marshal(input)
	if err != nil {
		return &sdktypes.ToolResult{IsError: true, Error: err.Error()}, nil
	}

	result, err := t.fn(ctx, json.RawMessage(argsJSON))
	if err != nil {
		// Execute already clipped the body and appended ErrorAnalyzeHint.
		// Do not concatenate err.Error() again — it can be an unclipped dump.
		if result == "" {
			result = err.Error()
		}
		return &sdktypes.ToolResult{
			IsError: true,
			Error:   result,
			Content: []sdktypes.ContentBlock{{
				Type: sdktypes.ContentBlockText,
				Text: result,
			}},
		}, nil
	}

	return &sdktypes.ToolResult{
		Content: []sdktypes.ContentBlock{{
			Type: sdktypes.ContentBlockText,
			Text: result,
		}},
	}, nil
}

func (t *toolAdapter) IsConcurrencySafe(input map[string]interface{}) bool {
	return readOnlyTools[t.name]
}

func (t *toolAdapter) IsReadOnly(input map[string]interface{}) bool {
	return readOnlyTools[t.name]
}

// sdkEngine wraps SDK components for concurrent tool execution and cost tracking.
type sdkEngine struct {
	costTracker *costtracker.Tracker
}

// newSDKEngine creates a new SDK engine with cost tracking.
func newSDKEngine(sessionID string) *sdkEngine {
	return &sdkEngine{
		costTracker: costtracker.NewTracker(sessionID),
	}
}

// buildSDKRegistry converts FastClaw's tool registry into an SDK registry.
// Callables go through Registry.Execute so clipToolResult and DenyIfHidden
// apply to the same path the loop uses.
func buildSDKRegistry(fcRegistry *tools.Registry) *sdktools.Registry {
	sdkReg := sdktools.NewRegistry()
	for _, def := range fcRegistry.Definitions() {
		name := def.Function.Name
		if fcRegistry.GetFunc(name) == nil {
			continue
		}
		sdkReg.Register(&toolAdapter{
			name:        name,
			description: def.Function.Description,
			params:      def.Function.Parameters,
			fn: func(ctx context.Context, args json.RawMessage) (string, error) {
				return fcRegistry.Execute(ctx, name, string(args))
			},
		})
	}
	return sdkReg
}

// toolCallResult holds the result of a single tool call with metadata.
type toolCallResult struct {
	toolCallID string
	toolName   string
	result     string
	err        error
}

const (
	toolNotEnabledResult = "Not executed: this tool is not enabled for the current model request."
	toolBadArgsResult    = "Not executed: tool arguments were not valid JSON."
)

// executeToolsConcurrently runs tool calls using the SDK's concurrent executor.
// allowed is the name set sent to the model this round; empty means nothing
// may run (tools were disabled for wrap-up / empty recovery).
func (e *sdkEngine) executeToolsConcurrently(ctx context.Context, fcRegistry *tools.Registry, toolCalls []provider.ToolCall, workspace string, allowed map[string]struct{}) []toolCallResult {
	results := make([]toolCallResult, len(toolCalls))
	var toRun []provider.ToolCall
	runIdx := make([]int, 0, len(toolCalls))
	for i, tc := range toolCalls {
		if _, ok := allowed[tc.Function.Name]; !ok {
			results[i] = toolCallResult{
				toolCallID: tc.ID,
				toolName:   tc.Function.Name,
				result:     toolNotEnabledResult,
				err:        fmt.Errorf("%s", toolNotEnabledResult),
			}
			continue
		}
		args := strings.TrimSpace(tc.Function.Arguments)
		if args == "" {
			tc.Function.Arguments = "{}"
			toolCalls[i].Function.Arguments = "{}"
		} else if !json.Valid([]byte(args)) {
			results[i] = toolCallResult{
				toolCallID: tc.ID,
				toolName:   tc.Function.Name,
				result:     toolBadArgsResult,
				err:        fmt.Errorf("%s", toolBadArgsResult),
			}
			continue
		}
		toRun = append(toRun, toolCalls[i])
		runIdx = append(runIdx, i)
	}
	if len(toRun) == 0 {
		return results
	}

	sdkReg := buildSDKRegistry(fcRegistry)
	executor := sdktools.NewExecutor(sdkReg, nil, &sdktypes.ToolUseContext{
		WorkingDir: workspace,
		AbortCtx:   ctx,
	})

	calls := make([]sdktools.ToolCallRequest, len(toRun))
	for i, tc := range toRun {
		var input map[string]interface{}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
			results[runIdx[i]] = toolCallResult{
				toolCallID: tc.ID,
				toolName:   tc.Function.Name,
				result:     toolBadArgsResult,
				err:        fmt.Errorf("%s", toolBadArgsResult),
			}
			calls[i] = sdktools.ToolCallRequest{ToolUseID: tc.ID, ToolName: "", Input: map[string]interface{}{}}
			continue
		}
		calls[i] = sdktools.ToolCallRequest{
			ToolUseID: tc.ID,
			ToolName:  tc.Function.Name,
			Input:     input,
		}
	}

	start := time.Now()
	responses := executor.RunTools(ctx, calls)
	e.costTracker.AddToolDuration(time.Since(start))

	// Anthropic (and OpenAI) require a tool_result for every tool_use the
	// model just emitted — orphaned tool_use IDs make the next API call
	// return 400 invalid_request_error. The SDK can short-circuit and
	// return fewer responses than requested (context cancel, executor
	// poisoned by a sandbox-creation failure, etc.), so build the result
	// slice keyed on toolCalls and look up by ToolUseID instead of zipping
	// position-by-position. Missing entries become explicit failure
	// tool_results so the conversation history stays well-formed.
	byID := make(map[string]sdktools.ToolCallResponse, len(responses))
	for _, resp := range responses {
		byID[resp.ToolUseID] = resp
	}
	for _, i := range runIdx {
		if results[i].toolCallID != "" && results[i].result != "" {
			continue
		}
		tc := toolCalls[i]
		resp, ok := byID[tc.ID]
		if !ok {
			results[i] = toolCallResult{
				toolCallID: tc.ID,
				toolName:   tc.Function.Name,
				result:     "tool execution did not return a result (sandbox or executor failure — check gateway logs)",
				err:        fmt.Errorf("no response from executor for tool_use %s", tc.ID),
			}
			continue
		}
		var resultText string
		if resp.Result != nil {
			if resp.Result.IsError {
				resultText = resp.Result.Error
				if resultText == "" && len(resp.Result.Content) > 0 {
					resultText = resp.Result.Content[0].Text
				}
				results[i] = toolCallResult{
					toolCallID: resp.ToolUseID,
					toolName:   tc.Function.Name,
					result:     resultText,
					err:        fmt.Errorf("%s", resultText),
				}
				continue
			}
			var parts []string
			for _, cb := range resp.Result.Content {
				if cb.Text != "" {
					parts = append(parts, cb.Text)
				}
			}
			resultText = strings.Join(parts, "\n")
		}
		if resp.Error != nil {
			results[i] = toolCallResult{
				toolCallID: resp.ToolUseID,
				toolName:   tc.Function.Name,
				result:     resultText,
				err:        resp.Error,
			}
		} else {
			results[i] = toolCallResult{
				toolCallID: resp.ToolUseID,
				toolName:   tc.Function.Name,
				result:     resultText,
			}
		}
	}
	return results
}
