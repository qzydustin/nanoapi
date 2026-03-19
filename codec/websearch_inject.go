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

func webSearchRawBlocks(id string, results []WebSearchResult) (json.RawMessage, json.RawMessage) {
	toolUse, _ := json.Marshal(map[string]any{
		"type": "server_tool_use", "id": id, "name": "web_search", "input": map[string]any{},
	})

	content := make([]any, len(results))
	for i, r := range results {
		content[i] = map[string]any{
			"type": "web_search_result", "url": r.URL, "title": r.Title,
			"encrypted_content": "", "page_age": nil,
		}
	}
	toolResult, _ := json.Marshal(map[string]any{
		"type": "web_search_tool_result", "tool_use_id": id, "content": content,
	})
	return toolUse, toolResult
}

// WebSearchResult holds a URL and title extracted from search results.
type WebSearchResult struct {
	URL   string
	Title string
}

// ParseOpenWebUISources extracts URLs and titles from an OpenWebUI sources event.
// Returns nil if the data is not a sources event.
func ParseOpenWebUISources(data string) []WebSearchResult {
	var envelope struct {
		Sources []struct {
			Source struct {
				Type string   `json:"type"`
				URLs []string `json:"urls"`
			} `json:"source"`
			Metadata []struct {
				Title  string `json:"title"`
				Source string `json:"source"`
			} `json:"metadata"`
		} `json:"sources"`
	}
	if json.Unmarshal([]byte(data), &envelope) != nil || len(envelope.Sources) == 0 {
		return nil
	}

	// Build title lookup from metadata.
	titleByURL := map[string]string{}
	for _, src := range envelope.Sources {
		for _, m := range src.Metadata {
			if m.Source != "" && m.Title != "" {
				titleByURL[m.Source] = m.Title
			}
		}
	}

	// Deduplicate URLs while preserving order.
	seen := map[string]bool{}
	var results []WebSearchResult
	for _, src := range envelope.Sources {
		if src.Source.Type != "web_search" {
			continue
		}
		for _, u := range src.Source.URLs {
			if seen[u] {
				continue
			}
			seen[u] = true
			results = append(results, WebSearchResult{URL: u, Title: titleByURL[u]})
		}
	}
	return results
}

// SynthesizeWebSearchBlocks returns synthetic server_tool_use and
// web_search_tool_result blocks for non-streaming Anthropic responses.
func SynthesizeWebSearchBlocks(results []WebSearchResult) (canonical.CanonicalContentBlock, canonical.CanonicalContentBlock) {
	toolUse, toolResult := webSearchRawBlocks(webSearchToolID(), results)
	return canonical.CanonicalContentBlock{Type: "server_tool_use", RawJSON: toolUse},
		canonical.CanonicalContentBlock{Type: "web_search_tool_result", RawJSON: toolResult}
}

// SynthesizeWebSearchSSE returns canonical stream events for synthetic web
// search blocks. Feed these through the encoder for correct index tracking.
func SynthesizeWebSearchSSE(results []WebSearchResult) []canonical.CanonicalStreamEvent {
	toolUse, toolResult := webSearchRawBlocks(webSearchToolID(), results)

	wrapBlock := func(block json.RawMessage) json.RawMessage {
		data, _ := json.Marshal(map[string]any{"content_block": json.RawMessage(block)})
		return data
	}

	return []canonical.CanonicalStreamEvent{
		{Type: canonical.EventRawBlockStart, RawJSON: wrapBlock(toolUse)},
		{Type: canonical.EventRawBlockStop},
		{Type: canonical.EventRawBlockStart, RawJSON: wrapBlock(toolResult)},
		{Type: canonical.EventRawBlockStop},
	}
}
