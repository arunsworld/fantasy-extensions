package fantasyextensions

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"charm.land/fantasy"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"
)

type SystemPromptGenerator func(context.Context) string

type ToolFetcher func(context.Context) []fantasy.AgentTool

type AgentContextValue string

const AgentContextStateKey = AgentContextValue("state")

type AGUIHandlerOptions struct {
	EmitReasoningEventsAs EmitReasoningAsEventType
	ProviderOptions       fantasy.ProviderOptions
}

type EmitReasoningAsEventType uint8

const (
	EmitReasoningAsThinkingEvents EmitReasoningAsEventType = iota
	EmitReasoningAsTextEvents
)

func AGUIHandler(model fantasy.LanguageModel, spg SystemPromptGenerator, toolFetcher ToolFetcher, options AGUIHandlerOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		var input aguiAgenticInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		threadID := input.ThreadID
		if threadID == "" {
			threadID = events.GenerateThreadID()
		}
		runID := input.RunID
		if runID == "" {
			runID = events.GenerateRunID()
		}
		state := input.State
		messages := input.toMessages()

		var agentContext context.Context
		if state != nil {
			agentContext = context.WithValue(r.Context(), AgentContextStateKey, state)
		} else {
			agentContext = r.Context()
		}

		var prompt string
		if len(messages) == 0 {
			prompt = "Hello!"
		}

		aguiTools := input.toTools()
		stopConditons := make([]fantasy.StopCondition, 0, len(aguiTools))
		for _, tool := range aguiTools {
			stopConditons = append(stopConditons, fantasy.HasToolCall(tool.Info().Name))
		}

		var tools []fantasy.AgentTool
		if toolFetcher != nil {
			tools = toolFetcher(agentContext)
		}

		agent := fantasy.NewAgent(
			model,
			fantasy.WithSystemPrompt(spg(agentContext)),
			fantasy.WithTools(aguiTools...),
			fantasy.WithTools(tools...),
		)

		messageIDs := make(map[string]string)
		reasoningIDs := make(map[string]string)

		streamWriter := newStreamWriter(w)
		if err := streamWriter.WriteEvent(r.Context(), events.NewRunStartedEvent(threadID, runID)); err != nil {
			log.Printf("error writing run started event: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		streamCall := fantasy.AgentStreamCall{
			Prompt: prompt,

			StopWhen: stopConditons,

			Messages: messages,

			ProviderOptions: options.ProviderOptions,

			OnReasoningStart: func(id string, reasoning fantasy.ReasoningContent) error {
				switch options.EmitReasoningEventsAs {
				case EmitReasoningAsThinkingEvents:
					e := events.NewThinkingStartEvent()
					if err := streamWriter.WriteEvent(r.Context(), e); err != nil {
						log.Printf("error writing reasoning start event: %v", err)
						return err
					}
					e2 := events.NewThinkingTextMessageStartEvent()
					if err := streamWriter.WriteEvent(r.Context(), e2); err != nil {
						log.Printf("error writing reasoning text message start event: %v", err)
						return err
					}
				case EmitReasoningAsTextEvents:
					reasoningIDs[id] = events.GenerateMessageID()
					e := events.NewTextMessageStartEvent(reasoningIDs[id], events.WithRole("assistant"))
					if err := streamWriter.WriteEvent(r.Context(), e); err != nil {
						log.Printf("error writing reasoning text started event: %v", err)
						return err
					}
					e2 := events.NewTextMessageContentEvent(reasoningIDs[id], "Reasoning: ")
					if err := streamWriter.WriteEvent(r.Context(), e2); err != nil {
						log.Printf("error writing reasoning text delta event: %v", err)
						return err
					}
				}
				return nil
			},

			OnReasoningDelta: func(id, text string) error {
				switch options.EmitReasoningEventsAs {
				case EmitReasoningAsThinkingEvents:
					e := events.NewThinkingTextMessageContentEvent(text)
					if err := streamWriter.WriteEvent(r.Context(), e); err != nil {
						log.Printf("error writing reasoning delta event: %v", err)
						return err
					}
				case EmitReasoningAsTextEvents:
					e := events.NewTextMessageContentEvent(reasoningIDs[id], text)
					if err := streamWriter.WriteEvent(r.Context(), e); err != nil {
						log.Printf("error writing reasoning text delta event: %v", err)
						return err
					}
				}
				return nil
			},

			OnReasoningEnd: func(id string, reasoning fantasy.ReasoningContent) error {
				switch options.EmitReasoningEventsAs {
				case EmitReasoningAsThinkingEvents:
					e := events.NewThinkingEndEvent()
					if err := streamWriter.WriteEvent(r.Context(), e); err != nil {
						log.Printf("error writing reasoning end event: %v", err)
						return err
					}
					e2 := events.NewThinkingTextMessageEndEvent()
					if err := streamWriter.WriteEvent(r.Context(), e2); err != nil {
						log.Printf("error writing reasoning text message end event: %v", err)
						return err
					}
				case EmitReasoningAsTextEvents:
					e := events.NewTextMessageEndEvent(reasoningIDs[id])
					if err := streamWriter.WriteEvent(r.Context(), e); err != nil {
						log.Printf("error writing reasoning text ended event: %v", err)
						return err
					}
					delete(reasoningIDs, id)
				}
				return nil
			},

			OnTextStart: func(id string) error {
				messageIDs[id] = events.GenerateMessageID()
				e := events.NewTextMessageStartEvent(messageIDs[id], events.WithRole("assistant"))
				if err := streamWriter.WriteEvent(r.Context(), e); err != nil {
					log.Printf("error writing text started event: %v", err)
					return err
				}
				return nil
			},

			OnTextDelta: func(id, text string) error {
				e := events.NewTextMessageContentEvent(messageIDs[id], text)
				if err := streamWriter.WriteEvent(r.Context(), e); err != nil {
					log.Printf("error writing text delta event: %v", err)
					return err
				}
				return nil
			},

			OnTextEnd: func(id string) error {
				e := events.NewTextMessageEndEvent(messageIDs[id])
				if err := streamWriter.WriteEvent(r.Context(), e); err != nil {
					log.Printf("error writing text ended event: %v", err)
					return err
				}
				delete(messageIDs, id)
				return nil
			},

			OnToolInputStart: func(id, toolName string) error {
				e := events.NewToolCallStartEvent(id, toolName)
				if err := streamWriter.WriteEvent(r.Context(), e); err != nil {
					log.Printf("error writing tool call start event: %v", err)
					return err
				}
				return nil
			},

			OnToolInputDelta: func(id, delta string) error {
				e := events.NewToolCallArgsEvent(id, delta)
				if err := streamWriter.WriteEvent(r.Context(), e); err != nil {
					log.Printf("error writing tool call content event: %v", err)
					return err
				}
				return nil
			},

			OnToolCall: func(toolCall fantasy.ToolCallContent) error {
				e := events.NewToolCallEndEvent(toolCall.ToolCallID)
				if err := streamWriter.WriteEvent(r.Context(), e); err != nil {
					log.Printf("error writing tool call end event: %v", err)
					return err
				}
				return nil
			},

			// When a tool call completes, send the result to the browser.
			// This should never return an error to avoid interrupting the agent flow.
			OnToolResult: func(res fantasy.ToolResultContent) error {
				if res.ClientMetadata != "" {
					var metadata map[string]any
					if err := json.Unmarshal([]byte(res.ClientMetadata), &metadata); err != nil {
						log.Printf("OnToolResult: error: failed to unmarshal client metadata: %v", err)
						return nil
					}
					// if the tool is an agui tool, we don't need to send the dummy response back to the browser
					if metadata["aguitool"] == true {
						return nil
					}
					// if the tool suggested an update to the state, send it to the browser
					if metadata["stateUpdate"] != nil {
						state = metadata["stateUpdate"]
						stateEvent := events.NewStateSnapshotEvent(state)
						if err := streamWriter.WriteEvent(r.Context(), stateEvent); err != nil {
							log.Printf("error writing state snapshot event: %v", err)
						}
					}
				}

				var content string
				switch res.Result.GetType() {
				case fantasy.ToolResultContentTypeText:
					text, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](res.Result)
					if !ok {
						return fmt.Errorf("failed to cast result to text")
					}
					content = text.Text
				case fantasy.ToolResultContentTypeError:
					toolErr, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentError](res.Result)
					if !ok {
						return fmt.Errorf("failed to cast result to json")
					}
					c, err := json.Marshal(struct {
						Error string `json:"error"`
					}{Error: toolErr.Error.Error()})
					if err != nil {
						log.Printf("OnToolResult: error: failed to marshal error: %v", err)
						content = fmt.Sprintf("error encountered: %s", toolErr.Error.Error())
					} else {
						content = string(c)
					}
				case fantasy.ToolResultContentTypeMedia:
					// media, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentMedia](res.Result)
					// if !ok {
					// 	return fmt.Errorf("failed to cast result to error")
					// }
					log.Printf("OnToolResult: error: media content not supported")
					return nil
				default:
					log.Printf("OnToolResult: error: unsupported content type: %s", res.Result.GetType())
					return nil
				}

				e := events.NewToolCallResultEvent(events.GenerateMessageID(), res.ToolCallID, content)
				if err := streamWriter.WriteEvent(r.Context(), e); err != nil {
					log.Printf("error writing tool call result event: %v", err)
					return nil
				}
				return nil
			},

			OnAgentFinish: func(result *fantasy.AgentResult) error {
				e := events.NewRunFinishedEvent(threadID, runID)
				if err := streamWriter.WriteEvent(r.Context(), e); err != nil {
					log.Printf("error writing run finished event: %v", err)
					return err
				}
				return nil
			},

			OnError: func(err error) {
				log.Printf("agent streaming on error: %v", err)
				e := events.NewRunErrorEvent(err.Error(), events.WithRunID(runID))
				if err := streamWriter.WriteEvent(r.Context(), e); err != nil {
					log.Printf("error writing run error event: %v", err)
				}
			},
		}

		// if options.ReasoningEffort != nil {
		// 	streamCall.ProviderOptions = fantasy.ProviderOptions{
		// 		openaicompat.Name: &openaicompat.ProviderOptions{
		// 			ReasoningEffort: options.ReasoningEffort,
		// 		},
		// 	}
		// }

		if _, err := agent.Stream(agentContext, streamCall); err != nil {
			log.Printf("error streaming agent: %v", err)
			return
		}
	}
}

type aguiAgenticInput struct {
	ThreadID       string           `json:"thread_id"`
	RunID          string           `json:"run_id"`
	State          any              `json:"state"`
	Messages       []map[string]any `json:"messages"`
	Tools          []aguiTool       `json:"tools"`
	Context        []any            `json:"context"`
	ForwardedProps any              `json:"forwarded_props"`
}

func (i *aguiAgenticInput) toMessages() []fantasy.Message {
	messages := make([]fantasy.Message, 0, len(i.Messages))
	for _, message := range i.Messages {
		msg := aguiMessageToFantasyMessage(message)
		if msg.Role == "" {
			continue
		}
		messages = append(messages, msg)
	}
	return messages
}

func (i *aguiAgenticInput) toTools() []fantasy.AgentTool {
	result := make([]fantasy.AgentTool, 0, len(i.Tools))
	for _, toolInfo := range i.Tools {
		result = append(result, &aguiFrontEndTool{
			toolInfo: fantasy.ToolInfo{
				Name:        toolInfo.Name,
				Description: toolInfo.Description,
				Parameters:  toolInfo.Parameters.Properties,
				Required:    toolInfo.Parameters.Required,
			},
			providerOptions: fantasy.ProviderOptions{},
		})
	}
	return result
}

type aguiFrontEndTool struct {
	toolInfo        fantasy.ToolInfo
	providerOptions fantasy.ProviderOptions
}

func (t *aguiFrontEndTool) Info() fantasy.ToolInfo {
	return t.toolInfo
}

func (t *aguiFrontEndTool) Run(_ context.Context, params fantasy.ToolCall) (fantasy.ToolResponse, error) {
	return fantasy.WithResponseMetadata(fantasy.ToolResponse{
		Type:    "string",
		Content: "",
	}, map[string]any{
		"aguitool": true,
	}), nil
}

func (t *aguiFrontEndTool) ProviderOptions() fantasy.ProviderOptions {
	return t.providerOptions
}

func (t *aguiFrontEndTool) SetProviderOptions(opts fantasy.ProviderOptions) {
	t.providerOptions = opts
}

type aguiTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  struct {
		Properties map[string]any `json:"properties"`
		Required   []string       `json:"required"`
		Type       string         `json:"type"`
	} `json:"parameters"`
}

func aguiMessageToFantasyMessage(message map[string]any) fantasy.Message {
	var role string
	if r, ok := message["role"]; ok {
		role = r.(string)
	}
	if role == "" {
		return fantasy.Message{}
	}
	switch role {
	case "user":
		return fantasy.Message{
			Role:    fantasy.MessageRoleUser,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: message["content"].(string)}},
		}
	case "assistant":
		if toolCalls, ok := message["toolCalls"]; ok {
			toolCallsSlice, ok := toolCalls.([]any)
			if ok {
				content := make([]fantasy.MessagePart, 0, len(toolCallsSlice))
				for _, toolCall := range toolCallsSlice {
					toolCallMap, ok := toolCall.(map[string]any)
					if ok {
						toolCallID := toolCallMap["id"].(string)
						function, ok := toolCallMap["function"].(map[string]any)
						if ok {
							toolName := function["name"].(string)
							toolInput := function["arguments"].(string)
							content = append(content, fantasy.ToolCallPart{
								ToolCallID: toolCallID,
								ToolName:   toolName,
								Input:      toolInput,
							})
						}
					}
				}
				return fantasy.Message{
					Role:    fantasy.MessageRoleAssistant,
					Content: content,
				}
			}
			return fantasy.Message{}
		}
		return fantasy.Message{
			Role:    fantasy.MessageRoleAssistant,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: message["content"].(string)}},
		}
	case "tool":
		return fantasy.Message{
			Role: fantasy.MessageRoleTool,
			Content: []fantasy.MessagePart{fantasy.ToolResultPart{
				ToolCallID: message["toolCallId"].(string),
				Output: fantasy.ToolResultOutputContentText{
					Text: message["content"].(string),
				},
			}},
		}
	default:
		return fantasy.Message{}
	}
}

type streamWriter struct {
	w http.ResponseWriter
	// internal
	sseWriter *sse.SSEWriter
	flusher   http.Flusher
}

func newStreamWriter(w http.ResponseWriter) *streamWriter {
	var flusher http.Flusher
	if f, ok := w.(http.Flusher); ok {
		flusher = f
	} else {
		log.Printf("warning: http.ResponseWriter does not implement http.Flusher")
	}
	return &streamWriter{
		w:         w,
		sseWriter: sse.NewSSEWriter(),
		flusher:   flusher,
	}
}

func (s *streamWriter) WriteEvent(ctx context.Context, event events.Event) error {
	if err := s.sseWriter.WriteEvent(ctx, s.w, event); err != nil {
		return err
	}
	if s.flusher != nil {
		s.flusher.Flush()
	}
	return nil
}
