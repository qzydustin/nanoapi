package codec

import (
	"encoding/json"
	"fmt"
	"math/rand"
)

func webSearchToolID() string {
	return fmt.Sprintf("srvtoolu_%016x", rand.Uint64())
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

// SynthesizeWebSearchBlocks returns a single ContentBlock representing the
// synthetic server_tool_use + web_search_tool_result pair.
func SynthesizeWebSearchBlocks(results []WebSearchResult) ContentBlock {
	return ContentBlock{
		Type:               "web_search",
		WebSearchToolUseID: webSearchToolID(),
		WebSearchResults:   append([]WebSearchResult(nil), results...),
	}
}

// SynthesizeWebSearchSSE returns a canonical stream event for synthetic web
// search blocks. Feed this through the encoder for correct index tracking.
func SynthesizeWebSearchSSE(results []WebSearchResult) StreamEvent {
	return StreamEvent{
		Type:               EventWebSearch,
		WebSearchToolUseID: webSearchToolID(),
		WebSearchResults:   append([]WebSearchResult(nil), results...),
	}
}
