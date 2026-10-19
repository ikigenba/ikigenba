package toolkit

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"github.com/ikigenba/ikigenba/agentkit"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

// WebFetchBodyBytes is the maximum number of response bytes fetched.
const WebFetchBodyBytes = 4194304

// WebFetchTimeout bounds a complete fetch, including redirects.
const WebFetchTimeout = 30 * time.Second

type webFetchInput struct {
	URL    string `json:"url" jsonschema:"required,minLength=1,description=Public URL to fetch"`
	Offset int    `json:"offset,omitempty" jsonschema:"minimum=0,description=Character offset in the fetched content"`
}

// WebFetch returns a public-URL fetching tool. Its default client ignores proxy
// environment variables and checks resolved addresses before each connection.
func WebFetch(opts ...WebFetchOption) (agentkit.Tool, error) {
	cfg := webConfig{client: defaultFetchClient()}
	for _, opt := range opts {
		opt.applyWebFetch(&cfg)
	}
	if cfg.client == nil {
		return nil, fmt.Errorf("HTTP client must not be nil")
	}
	// Copy the client so neither the redirect policy nor other caller state changes.
	client := *cfg.client
	client.CheckRedirect = func(_ *http.Request, via []*http.Request) error {
		if len(via) > 10 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	}
	return agentkit.NewTool[webFetchInput]("WebFetch", "Fetch a public URL as text or Markdown", capOutput(func(ctx context.Context, in webFetchInput) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, WebFetchTimeout)
		defer cancel()
		result, err := fetchContent(ctx, &client, in)
		if err != nil {
			return "", fmt.Errorf("url %q: %w", in.URL, err)
		}
		return result, nil
	}), func(webFetchInput) agentkit.Access { return agentkit.BlocksNone() })
}

func defaultFetchClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = guardedFetchDial
	return &http.Client{Transport: transport, Timeout: WebFetchTimeout}
}

func guardedFetchDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	for _, addr := range addresses {
		ip := addr.Unmap()
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast() || netip.MustParsePrefix("0.0.0.0/8").Contains(ip) || netip.MustParsePrefix("100.64.0.0/10").Contains(ip) {
			return nil, fmt.Errorf("refused non-public address %s", addr)
		}
	}
	var lastErr error
	for _, addr := range addresses {
		conn, dialErr := (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no addresses for %s", host)
	}
	return nil, lastErr
}

func fetchContent(ctx context.Context, client *http.Client, in webFetchInput) (string, error) {
	parsed, err := url.Parse(in.URL)
	if err != nil {
		return "", err
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if !parsed.IsAbs() || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("expected an absolute http or https URL with a host")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", err
	}
	response, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return "", fmt.Errorf("HTTP status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, WebFetchBodyBytes))
	if err != nil {
		return "", err
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType == "" {
		mediaType, _, _ = mime.ParseMediaType(http.DetectContentType(body[:min(512, len(body))]))
	}
	mediaType = strings.ToLower(mediaType)
	isHTML := mediaType == "text/html" || mediaType == "application/xhtml+xml"
	if !isHTML && !strings.HasPrefix(mediaType, "text/") && mediaType != "application/json" && mediaType != "application/xml" && mediaType != "application/javascript" {
		return "", fmt.Errorf("unsupported media type %s", mediaType)
	}
	content, err := decodeFetchBody(body, response.Header.Get("Content-Type"))
	if err != nil {
		return "", err
	}
	finalURL := response.Request.URL.String()
	if isHTML {
		conv := converter.NewConverter(converter.WithPlugins(base.NewBasePlugin(), commonmark.NewCommonmarkPlugin(commonmark.WithHeadingStyle(commonmark.HeadingStyleATX), commonmark.WithBulletListMarker("-"), commonmark.WithEmDelimiter("*"), commonmark.WithListEndComment(false)), table.NewTablePlugin()))
		content, err = conv.ConvertString(content, converter.WithDomain(finalURL), converter.WithContext(ctx))
		if err != nil {
			return "", err
		}
	}
	header := fmt.Sprintf("URL: %s\nStatus: %d\nType: %s\n\n", finalURL, response.StatusCode, mediaType)
	runes := []rune(content)
	if in.Offset >= len(runes) {
		return header + fmt.Sprintf("[no content at offset %d of %d characters]", in.Offset, len(runes)), nil
	}
	return header + string(runes[in.Offset:]), nil
}

func decodeFetchBody(body []byte, contentType string) (string, error) {
	enc, _, certain := charset.DetermineEncoding(body, contentType)
	if !certain && !fetchHasMetaCharset(body[:min(1024, len(body))]) {
		complete := body
		for len(complete) > 0 && !utf8.Valid(complete) {
			start := len(complete) - 1
			for start > 0 && !utf8.RuneStart(complete[start]) {
				start--
			}
			if utf8.FullRune(complete[start:]) {
				break
			}
			complete = complete[:start]
		}
		if utf8.Valid(complete) {
			return string(complete), nil
		}
		enc, _ = charset.Lookup("windows-1252")
	}
	decoded, err := enc.NewDecoder().Bytes(body)
	return strings.TrimPrefix(string(decoded), "\ufeff"), err
}

func fetchHasMetaCharset(preview []byte) bool {
	tokenizer := html.NewTokenizer(bytes.NewReader(preview))
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return false
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			if token.Data != "meta" {
				continue
			}
			attrs := make(map[string]string)
			for _, attr := range token.Attr {
				if _, exists := attrs[attr.Key]; !exists {
					attrs[attr.Key] = attr.Val
				}
			}
			if enc, _ := charset.Lookup(attrs["charset"]); enc != nil {
				return true
			}
			if strings.EqualFold(attrs["http-equiv"], "content-type") {
				_, params, err := mime.ParseMediaType(attrs["content"])
				if err == nil {
					if enc, _ := charset.Lookup(params["charset"]); enc != nil {
						return true
					}
				}
			}
		}
	}
}
