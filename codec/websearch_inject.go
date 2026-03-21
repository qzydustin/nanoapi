package codec

import (
	"encoding/json"
	"fmt"
	"math/rand"
)

func webSearchToolID() string {
	return fmt.Sprintf("srvtoolu_%016x", rand.Uint64())
}

func newWebSearchSynthetic(toolUseID string, results []WebSearchResult) (*ServerToolUse, *WebSearchToolResult) {
	clonedResults := append([]WebSearchResult(nil), results...)
	serverToolUse := &ServerToolUse{
		ID:    toolUseID,
		Name:  "web_search",
		Input: map[string]any{},
	}
	searchResult := &WebSearchToolResult{
		ToolUseID: toolUseID,
		Content:   clonedResults,
	}
	return serverToolUse, searchResult
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
func SynthesizeWebSearchBlocks(results []WebSearchResult) (ContentBlock, ContentBlock) {
	serverToolUse, searchResult := newWebSearchSynthetic(webSearchToolID(), results)
	toolUseBlock := ContentBlock{
		Type:          "server_tool_use",
		ServerToolUse: serverToolUse,
	}
	searchResultBlock := ContentBlock{
		Type:                "web_search_tool_result",
		WebSearchToolResult: searchResult,
	}
	return toolUseBlock, searchResultBlock
}

// SynthesizeWebSearchSSE returns canonical stream events for synthetic web
// search blocks. Feed these through the encoder for correct index tracking.
func SynthesizeWebSearchSSE(results []WebSearchResult) []StreamEvent {
	serverToolUse, searchResult := newWebSearchSynthetic(webSearchToolID(), results)

	return []StreamEvent{
		{
			Type:          EventServerToolUse,
			ServerToolUse: serverToolUse,
		},
		{
			Type:                EventWebSearchResult,
			WebSearchToolResult: searchResult,
		},
	}
}
