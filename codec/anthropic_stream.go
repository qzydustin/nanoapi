package codec

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/qzydustin/nanoapi/canonical"
)

// ---------------------------------------------------------------------------
// Anthropic Stream Chunk Decoding
// ---------------------------------------------------------------------------

// AnthropicStreamDecoder maintains state across Anthropic SSE events.
type AnthropicStreamDecoder struct {
	currentBlockType string
	currentBlockIdx  int
}

// NewAnthropicStreamDecoder creates a new decoder for Anthropic SSE.
func NewAnthropicStreamDecoder() *AnthropicStreamDecoder {
	return &AnthropicStreamDecoder{currentBlockIdx: -1}
}

// DecodeLine parses a single SSE line pair (event + data) from an Anthropic
// stream. Call this once per complete SSE event.
//
// eventType is from the "event: " line; data is from the "data: " line.
func (d *AnthropicStreamDecoder) DecodeLine(eventType string, data string) ([]canonical.CanonicalStreamEvent, error) {
	data = strings.TrimSpace(data)
	if data == "" {
		return nil, nil
	}

	var events []canonical.CanonicalStreamEvent

	switch eventType {
	case "message_start":
		var msg struct {
			Message struct {
				ID    string          `json:"id"`
				Model string          `json:"model"`
				Usage *anthropicUsage `json:"usage,omitempty"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(data), &msg); err != nil {
			return nil, fmt.Errorf("decode message_start: %w", err)
		}
		// We embed metadata via a synthetic text_delta with empty text that
		// carries ResponseID/Model. The consumer picks metadata from the first event.
		// However, to keep events clean, we'll just tag the first real event.
		// For now, store state. The next content event will carry these.
		// Actually, let's emit a minimal event to propagate metadata:
		events = append(events, canonical.CanonicalStreamEvent{
			Type:       canonical.EventTextDelta,
			Text:       "",
			ResponseID: msg.Message.ID,
			Model:      msg.Message.Model,
		})

	case "content_block_start":
		var block struct {
			Index        int             `json:"index"`
			ContentBlock json.RawMessage `json:"content_block"`
		}
		if err := json.Unmarshal([]byte(data), &block); err != nil {
			return nil, fmt.Errorf("decode content_block_start: %w", err)
		}

		var blockHeader struct {
			Type string `json:"type"`
			ID   string `json:"id,omitempty"`
			Name string `json:"name,omitempty"`
		}
		json.Unmarshal(block.ContentBlock, &blockHeader)

		d.currentBlockType = blockHeader.Type
		d.currentBlockIdx = block.Index

		switch blockHeader.Type {
		case "tool_use":
			events = append(events, canonical.CanonicalStreamEvent{
				Type:         canonical.EventToolCallStart,
				ToolCallID:   blockHeader.ID,
				ToolCallName: blockHeader.Name,
			})
		case "text", "thinking":
			// No start event needed, deltas suffice.
		default:
			events = append(events, canonical.CanonicalStreamEvent{
				Type:    canonical.EventRawBlockStart,
				RawJSON: []byte(data),
			})
		}

	case "content_block_delta":
		// Pass unrecognised block types through as-is.
		if d.currentBlockType != "text" && d.currentBlockType != "thinking" &&
			d.currentBlockType != "tool_use" {
			events = append(events, canonical.CanonicalStreamEvent{
				Type:    canonical.EventRawBlockDelta,
				RawJSON: []byte(data),
			})
			break
		}

		var delta struct {
			Delta struct {
				Type      string `json:"type"`
				Text      string `json:"text,omitempty"`
				Thinking  string `json:"thinking,omitempty"`
				Signature string `json:"signature,omitempty"`
				JSON      string `json:"partial_json,omitempty"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &delta); err != nil {
			return nil, fmt.Errorf("decode content_block_delta: %w", err)
		}

		switch delta.Delta.Type {
		case "text_delta":
			events = append(events, canonical.CanonicalStreamEvent{
				Type: canonical.EventTextDelta,
				Text: delta.Delta.Text,
			})
		case "thinking_delta":
			text := delta.Delta.Thinking
			if text == "" {
				text = delta.Delta.Text
			}
			events = append(events, canonical.CanonicalStreamEvent{
				Type: canonical.EventThinkingDelta,
				Text: text,
			})
		case "signature_delta":
			events = append(events, canonical.CanonicalStreamEvent{
				Type:      canonical.EventThinkingSignature,
				Signature: delta.Delta.Signature,
			})
		case "input_json_delta":
			events = append(events, canonical.CanonicalStreamEvent{
				Type:           canonical.EventToolCallDelta,
				ArgumentsDelta: delta.Delta.JSON,
			})
		}

	case "content_block_stop":
		if d.currentBlockType == "tool_use" {
			events = append(events, canonical.CanonicalStreamEvent{
				Type: canonical.EventToolCallEnd,
			})
		} else if d.currentBlockType != "text" && d.currentBlockType != "thinking" {
			events = append(events, canonical.CanonicalStreamEvent{
				Type:    canonical.EventRawBlockStop,
				RawJSON: []byte(data),
			})
		}
		d.currentBlockType = ""

	case "message_delta":
		var msg struct {
			Delta struct {
				StopReason string `json:"stop_reason,omitempty"`
			} `json:"delta"`
			Usage *anthropicUsage `json:"usage,omitempty"`
		}
		if err := json.Unmarshal([]byte(data), &msg); err != nil {
			return nil, fmt.Errorf("decode message_delta: %w", err)
		}
		if msg.Delta.StopReason != "" {
			events = append(events, canonical.CanonicalStreamEvent{
				Type:       canonical.EventMessageStop,
				StopReason: msg.Delta.StopReason,
			})
		}
		if msg.Usage != nil {
			in64 := int64(msg.Usage.InputTokens)
			out64 := int64(msg.Usage.OutputTokens)
			total64 := in64 + out64
			usage := &canonical.CanonicalUsage{
				InputTokens:  &in64,
				OutputTokens: &out64,
				TotalTokens:  &total64,
			}
			if msg.Usage.CacheRead != nil {
				cacheRead64 := int64(*msg.Usage.CacheRead)
				usage.CacheReadTokens = &cacheRead64
			}
			if msg.Usage.CacheCreate != nil {
				cacheWrite64 := int64(*msg.Usage.CacheCreate)
				usage.CacheWriteTokens = &cacheWrite64
			}
			events = append(events, canonical.CanonicalStreamEvent{
				Type:  canonical.EventUsageFinal,
				Usage: usage,
			})
		}

	case "message_stop":
		// End of stream signal. No canonical event needed.

	case "ping":
		// Keep-alive, ignore.
	}

	return events, nil
}

// ---------------------------------------------------------------------------
// Anthropic Stream Encoding (canonical event → SSE for client)
// ---------------------------------------------------------------------------

// AnthropicStreamEncoder tracks state for client-side Anthropic SSE encoding.
type AnthropicStreamEncoder struct {
	messageStarted bool
	blockIdx       int
	blockOpen      bool
	blockType      string
	stopReason     string
	usage          *canonical.CanonicalUsage
	messageDeltaEmitted bool
}

// NewAnthropicStreamEncoder creates a new encoder for client-side Anthropic SSE.
func NewAnthropicStreamEncoder() *AnthropicStreamEncoder {
	return &AnthropicStreamEncoder{}
}

// Encode converts a canonical stream event into Anthropic SSE line(s).
func (e *AnthropicStreamEncoder) Encode(event canonical.CanonicalStreamEvent) string {
	var lines []string

	// Emit message_start on the first real event.
	if !e.messageStarted && (event.ResponseID != "" || event.Model != "") {
		msgStart := map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":            event.ResponseID,
				"type":          "message",
				"role":          "assistant",
				"model":         event.Model,
				"content":       []any{},
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage": map[string]any{
					"input_tokens":  0,
					"output_tokens": 0,
				},
			},
		}
		lines = append(lines, anthropicSSE("message_start", msgStart))
		e.messageStarted = true
	}

	switch event.Type {
	case canonical.EventTextDelta:
		if event.Text == "" {
			break // skip empty metadata-only events
		}
		e.ensureBlockStart(&lines, "text")
		delta := map[string]any{
			"type":  "content_block_delta",
			"index": e.blockIdx,
			"delta": map[string]any{"type": "text_delta", "text": event.Text},
		}
		lines = append(lines, anthropicSSE("content_block_delta", delta))

	case canonical.EventThinkingDelta:
		e.ensureBlockStart(&lines, "thinking")
		delta := map[string]any{
			"type":  "content_block_delta",
			"index": e.blockIdx,
			"delta": map[string]any{"type": "thinking_delta", "thinking": event.Text},
		}
		lines = append(lines, anthropicSSE("content_block_delta", delta))

	case canonical.EventThinkingSignature:
		e.ensureBlockStart(&lines, "thinking")
		if event.Signature == "" {
			break
		}
		delta := map[string]any{
			"type":  "content_block_delta",
			"index": e.blockIdx,
			"delta": map[string]any{"type": "signature_delta", "signature": event.Signature},
		}
		lines = append(lines, anthropicSSE("content_block_delta", delta))

	case canonical.EventToolCallStart:
		e.closeBlock(&lines)
		start := map[string]any{
			"type":  "content_block_start",
			"index": e.blockIdx,
			"content_block": map[string]any{
				"type": "tool_use", "id": event.ToolCallID, "name": event.ToolCallName,
				"input": map[string]any{},
			},
		}
		lines = append(lines, anthropicSSE("content_block_start", start))
		e.blockOpen = true

	case canonical.EventToolCallDelta:
		delta := map[string]any{
			"type":  "content_block_delta",
			"index": e.blockIdx,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": event.ArgumentsDelta},
		}
		lines = append(lines, anthropicSSE("content_block_delta", delta))

	case canonical.EventToolCallEnd:
		e.closeBlock(&lines)

	case canonical.EventRawBlockStart:
		e.closeBlock(&lines)
		lines = append(lines, "event: content_block_start\ndata: " + string(event.RawJSON) + "\n\n")
		e.blockOpen = true
		e.blockType = "raw"

	case canonical.EventRawBlockDelta:
		lines = append(lines, "event: content_block_delta\ndata: " + string(event.RawJSON) + "\n\n")

	case canonical.EventRawBlockStop:
		lines = append(lines, "event: content_block_stop\ndata: " + string(event.RawJSON) + "\n\n")
		e.blockOpen = false
		e.blockType = ""
		e.blockIdx++

	case canonical.EventMessageStop:
		e.closeBlock(&lines)
		e.stopReason = event.StopReason
		if e.usage != nil && !e.messageDeltaEmitted {
			lines = append(lines, e.emitMessageDelta())
		}

	case canonical.EventUsageFinal:
		e.usage = event.Usage
		if e.stopReason != "" && !e.messageDeltaEmitted {
			lines = append(lines, e.emitMessageDelta())
		}
	}

	return strings.Join(lines, "")
}

// Done returns the final Anthropic SSE termination events.
func (e *AnthropicStreamEncoder) Done() string {
	var lines []string
	e.closeBlock(&lines)
	if !e.messageDeltaEmitted && (e.stopReason != "" || e.usage != nil) {
		lines = append(lines, e.emitMessageDelta())
	}
	lines = append(lines, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	return strings.Join(lines, "")
}

func (e *AnthropicStreamEncoder) ensureBlockStart(lines *[]string, blockType string) {
	if e.blockOpen {
		if e.blockType == blockType {
			return
		}
		e.closeBlock(lines)
	}
	start := map[string]any{
		"type":          "content_block_start",
		"index":         e.blockIdx,
		"content_block": map[string]any{"type": blockType},
	}
	if blockType == "text" {
		start["content_block"] = map[string]any{"type": "text", "text": ""}
	} else if blockType == "thinking" {
		start["content_block"] = map[string]any{"type": "thinking", "thinking": ""}
	}
	*lines = append(*lines, anthropicSSE("content_block_start", start))
	e.blockOpen = true
	e.blockType = blockType
}

func (e *AnthropicStreamEncoder) closeBlock(lines *[]string) {
	if !e.blockOpen {
		return
	}
	stop := map[string]any{"type": "content_block_stop", "index": e.blockIdx}
	*lines = append(*lines, anthropicSSE("content_block_stop", stop))
	e.blockOpen = false
	e.blockType = ""
	e.blockIdx++
}


func (e *AnthropicStreamEncoder) emitMessageDelta() string {
	u := map[string]any{
		"input_tokens":  0,
		"output_tokens": 0,
	}
	if e.usage != nil {
		if e.usage.InputTokens != nil {
			u["input_tokens"] = *e.usage.InputTokens
		}
		if e.usage.OutputTokens != nil {
			u["output_tokens"] = *e.usage.OutputTokens
		}
		if e.usage.CacheWriteTokens != nil {
			u["cache_creation_input_tokens"] = *e.usage.CacheWriteTokens
		}
		if e.usage.CacheReadTokens != nil {
			u["cache_read_input_tokens"] = *e.usage.CacheReadTokens
		}
	}
	stopReason := e.stopReason
	if stopReason == "" {
		stopReason = "end_turn"
	}
	msg := map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": u,
	}
	e.messageDeltaEmitted = true
	return anthropicSSE("message_delta", msg)
}

func anthropicSSE(event string, payload any) string {
	return "event: " + event + "\ndata: " + mustJSON(payload) + "\n\n"
}
