package codec

import (
	"encoding/json"
	"fmt"
	"math/rand"

	"github.com/qzydustin/nanoapi/canonical"
)

func webSearchToolID() string {
	return fmt.Sprintf("srvtoolu_%016x", rand.Uint64())
}

func webSearchRawBlocks(id string) (json.RawMessage, json.RawMessage) {
	toolUse, _ := json.Marshal(map[string]any{
		"type": "server_tool_use", "id": id, "name": "web_search", "input": map[string]any{},
	})
	toolResult, _ := json.Marshal(map[string]any{
		"type": "web_search_tool_result", "tool_use_id": id, "content": []any{},
	})
	return toolUse, toolResult
}

// SynthesizeWebSearchBlocks returns synthetic server_tool_use and
// web_search_tool_result blocks for non-streaming Anthropic responses.
func SynthesizeWebSearchBlocks() (canonical.CanonicalContentBlock, canonical.CanonicalContentBlock) {
	toolUse, toolResult := webSearchRawBlocks(webSearchToolID())
	return canonical.CanonicalContentBlock{Type: "server_tool_use", RawJSON: toolUse},
		canonical.CanonicalContentBlock{Type: "web_search_tool_result", RawJSON: toolResult}
}

// SynthesizeWebSearchSSE returns SSE lines for synthetic web search blocks
// in a streaming Anthropic response. Returns the SSE string and the number
// of blocks injected.
func SynthesizeWebSearchSSE(startIdx int) (string, int) {
	toolUse, toolResult := webSearchRawBlocks(webSearchToolID())

	sseBlock := func(idx int, block json.RawMessage) string {
		start, _ := json.Marshal(map[string]any{
			"type": "content_block_start", "index": idx,
			"content_block": json.RawMessage(block),
		})
		stop, _ := json.Marshal(map[string]any{
			"type": "content_block_stop", "index": idx,
		})
		return "event: content_block_start\ndata: " + string(start) + "\n\n" +
			"event: content_block_stop\ndata: " + string(stop) + "\n\n"
	}

	return sseBlock(startIdx, toolUse) + sseBlock(startIdx+1, toolResult), 2
}
