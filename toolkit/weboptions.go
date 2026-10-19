package toolkit

import "net/http"

// WebSearchOption configures a WebSearch tool.
type WebSearchOption interface {
	applyWebSearch(*webConfig)
}

// WebFetchOption configures a WebFetch tool.
type WebFetchOption interface {
	applyWebFetch(*webConfig)
}

type webConfig struct {
	client *http.Client
}

// HTTPClientOption supplies a client to either web tool.
type HTTPClientOption struct {
	client *http.Client
}

// WithHTTPClient replaces the web tool's HTTP client. The caller is responsible
// for any address restrictions: WebFetch's default network guard is not applied
// when a client is supplied.
func WithHTTPClient(client *http.Client) HTTPClientOption {
	return HTTPClientOption{client: client}
}

func (option HTTPClientOption) applyWebSearch(config *webConfig) {
	config.client = option.client
}

func (option HTTPClientOption) applyWebFetch(config *webConfig) {
	config.client = option.client
}
