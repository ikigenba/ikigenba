package toolkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/agentkit"
)

const braveSearchURL = "https://api.search.brave.com/res/v1/web/search"
const webSearchTimeout = 30 * time.Second

type webSearchInput struct {
	Query          string   `json:"query" jsonschema:"required,minLength=1,description=Search query"`
	AllowedDomains []string `json:"allowed_domains,omitempty" jsonschema:"description=Only include results from these domains"`
	BlockedDomains []string `json:"blocked_domains,omitempty" jsonschema:"description=Exclude results from these domains"`
}

type braveResult struct {
	Title         string   `json:"title"`
	URL           string   `json:"url"`
	Description   string   `json:"description"`
	ExtraSnippets []string `json:"extra_snippets"`
	Age           string   `json:"age"`
}

// WebSearch returns a tool that queries Brave with the supplied subscription key.
func WebSearch(key string, opts ...WebSearchOption) (agentkit.Tool, error) {
	if strings.TrimSpace(key) == "" {
		return nil, fmt.Errorf("key must not be blank")
	}
	config := webConfig{client: &http.Client{Timeout: webSearchTimeout}}
	for _, opt := range opts {
		opt.applyWebSearch(&config)
	}
	if config.client == nil {
		return nil, fmt.Errorf("HTTP client must not be nil")
	}
	client := *config.client
	// A search makes one request, including when Brave answers with a redirect.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return agentkit.NewTool[webSearchInput]("WebSearch", "Search the web using Brave Search", capOutput(func(ctx context.Context, input webSearchInput) (string, error) {
		return runWebSearch(ctx, &client, key, input)
	}), func(webSearchInput) agentkit.Access { return agentkit.BlocksNone() })
}

func runWebSearch(ctx context.Context, client *http.Client, key string, input webSearchInput) (string, error) {
	params := url.Values{"q": {input.Query}, "count": {"20"}, "extra_snippets": {"true"}, "country": {"us"}, "search_lang": {"en"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, braveSearchURL+"?"+params.Encode(), nil)
	if err != nil {
		return "", fmt.Errorf("query %q: %w", input.Query, err)
	}
	req.Header.Set("X-Subscription-Token", key)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("query %q: %w", input.Query, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		statusErr := braveStatusError(resp.StatusCode, body)
		if err != nil {
			return "", errors.Join(statusErr, fmt.Errorf("read search response: %w", err))
		}
		return "", statusErr
	}
	if err != nil {
		return "", fmt.Errorf("read search response: %w", err)
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 || body[0] != '{' {
		return "", fmt.Errorf("search response is not a JSON object")
	}
	var response struct {
		Web struct {
			Results []braveResult `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("decode search response: %w", err)
	}
	blocks := make([]string, 0, 10)
	for _, result := range response.Web.Results {
		parsed, err := url.Parse(result.URL)
		host := ""
		if err == nil {
			host = parsed.Hostname()
		}
		if matchesSearchDomain(host, input.BlockedDomains) || (len(input.AllowedDomains) > 0 && !matchesSearchDomain(host, input.AllowedDomains)) {
			continue
		}
		lines := []string{strconv.Itoa(len(blocks)+1) + ". " + searchField(result.Title), searchField(result.URL)}
		if result.Description != "" {
			lines = append(lines, searchField(result.Description))
		}
		for _, snippet := range result.ExtraSnippets {
			if snippet != "" {
				lines = append(lines, searchField(snippet))
			}
		}
		if result.Age != "" {
			lines = append(lines, "Age: "+searchField(result.Age))
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
		if len(blocks) == 10 {
			break
		}
	}
	if len(blocks) == 0 {
		return "No results.", nil
	}
	return strings.Join(blocks, "\n\n"), nil
}

func matchesSearchDomain(host string, domains []string) bool {
	if host == "" {
		return false
	}
	host = strings.ToLower(host)
	for _, domain := range domains {
		domain = strings.ToLower(domain)
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func searchField(field string) string {
	return strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(field)
}

func braveStatusError(status int, body []byte) error {
	var response struct {
		Type  string `json:"type"`
		Error *struct {
			Code   *string `json:"code"`
			Detail *string `json:"detail"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &response) == nil && response.Type == "ErrorResponse" && response.Error != nil && response.Error.Code != nil && response.Error.Detail != nil {
		return fmt.Errorf("brave status %d: %s: %s", status, *response.Error.Code, *response.Error.Detail)
	}
	if index := bytes.IndexAny(body, "\r\n"); index >= 0 {
		return fmt.Errorf("brave status %d: %s", status, body[:index])
	}
	return fmt.Errorf("brave status %d: %s", status, body)
}
