package execute

import (
	"fmt"

	"github.com/qzydustin/nanoapi/canonical"
	"github.com/qzydustin/nanoapi/config"
)

// BuildUpstreamURL constructs the upstream API endpoint URL.
func BuildUpstreamURL(baseURL string, protocol canonical.Protocol) string {
	// Remove trailing slash from base URL.
	for len(baseURL) > 0 && baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}

	switch protocol {
	case canonical.ProtocolOpenAIChat:
		return baseURL + "/v1/chat/completions"
	case canonical.ProtocolAnthropicMessage:
		return baseURL + "/v1/messages"
	default:
		return baseURL
	}
}

// BuildHeaders constructs the upstream request headers from provider config.
func BuildHeaders(provider *config.ProviderConfig, protocol canonical.Protocol, stream bool) map[string]string {
	headers := map[string]string{
		"Content-Type": "application/json",
	}

	if stream {
		headers["Accept"] = "text/event-stream"
	}

	apiKey := provider.APIKey
	switch protocol {
	case canonical.ProtocolOpenAIChat:
		headers["Authorization"] = fmt.Sprintf("Bearer %s", apiKey)
	case canonical.ProtocolAnthropicMessage:
		headers["x-api-key"] = apiKey
	}

	// Merge provider-level static headers.
	for k, v := range provider.Headers {
		headers[k] = v
	}

	return headers
}
