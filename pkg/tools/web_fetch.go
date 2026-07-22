package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/n-seiji/ebiclaw/pkg/config"
	"github.com/n-seiji/ebiclaw/pkg/logger"
	"github.com/n-seiji/ebiclaw/pkg/utils"
)

type WebFetchTool struct {
	maxChars        int
	proxy           string
	client          *http.Client
	format          string
	fetchLimitBytes int64
	whitelist       *privateHostWhitelist
}

type privateHostWhitelist struct {
	exact map[string]struct{}
	cidrs []*net.IPNet
}

func NewWebFetchTool(maxChars int, format string, fetchLimitBytes int64) (*WebFetchTool, error) {
	// createHTTPClient cannot fail with an empty proxy string.
	return NewWebFetchToolWithConfig(maxChars, "", format, fetchLimitBytes, nil)
}

// allowPrivateWebFetchHosts controls whether loopback/private hosts are allowed.
// This is false in normal runtime to reduce SSRF exposure, and tests can override it temporarily.
var allowPrivateWebFetchHosts atomic.Bool

func NewWebFetchToolWithProxy(
	maxChars int,
	proxy string,
	format string,
	fetchLimitBytes int64,
	privateHostWhitelist []string,
) (*WebFetchTool, error) {
	return NewWebFetchToolWithConfig(maxChars, proxy, format, fetchLimitBytes, privateHostWhitelist)
}

func NewWebFetchToolWithConfig(
	maxChars int,
	proxy string,
	format string,
	fetchLimitBytes int64,
	privateHostWhitelist []string,
) (*WebFetchTool, error) {
	if maxChars <= 0 {
		maxChars = defaultMaxChars
	}
	whitelist, err := newPrivateHostWhitelist(privateHostWhitelist)
	if err != nil {
		return nil, fmt.Errorf("failed to parse web fetch private host whitelist: %w", err)
	}
	client, err := utils.CreateHTTPClient(proxy, fetchTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client for web fetch: %w", err)
	}
	if transport, ok := client.Transport.(*http.Transport); ok {
		dialer := &net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		transport.DialContext = newSafeDialContext(dialer, whitelist)
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		if isObviousPrivateHost(req.URL.Hostname(), whitelist) {
			return fmt.Errorf("redirect target is private or local network host")
		}
		return nil
	}
	if fetchLimitBytes <= 0 {
		fetchLimitBytes = 10 * 1024 * 1024 // Security Fallback
	}
	return &WebFetchTool{
		maxChars:        maxChars,
		proxy:           proxy,
		client:          client,
		format:          format,
		fetchLimitBytes: fetchLimitBytes,
		whitelist:       whitelist,
	}, nil
}

func (t *WebFetchTool) Name() string {
	return "web_fetch"
}

func (t *WebFetchTool) Description() string {
	return "Fetch a URL and extract readable content (HTML to text). Use this to get weather info, news, articles, or any web content."
}

func (t *WebFetchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "URL to fetch",
			},
			"maxChars": map[string]any{
				"type":        "integer",
				"description": "Maximum characters to extract",
				"minimum":     100.0,
			},
		},
		"required": []string{"url"},
	}
}

func (t *WebFetchTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	urlStr, ok := args["url"].(string)
	if !ok {
		return ErrorResult("url is required")
	}

	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid URL: %v", err))
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return ErrorResult("only http/https URLs are allowed")
	}

	if parsedURL.Host == "" {
		return ErrorResult("missing domain in URL")
	}

	// Lightweight pre-flight: block obvious localhost/literal-IP without DNS resolution.
	// The real SSRF guard is newSafeDialContext at connect time.
	hostname := parsedURL.Hostname()
	if isObviousPrivateHost(hostname, t.whitelist) {
		return ErrorResult("fetching private or local network hosts is not allowed")
	}

	maxChars := t.maxChars
	if mc, ok := args["maxChars"].(float64); ok {
		if int(mc) > 100 {
			maxChars = int(mc)
		}
	}

	doFetch := func(ua string) (*http.Response, []byte, error) {
		req, reqErr := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
		if reqErr != nil {
			return nil, nil, fmt.Errorf("failed to create request: %w", reqErr)
		}
		req.Header.Set("User-Agent", ua)
		resp, doErr := t.client.Do(req)
		if doErr != nil {
			return nil, nil, fmt.Errorf("request failed: %w", doErr)
		}
		resp.Body = http.MaxBytesReader(nil, resp.Body, t.fetchLimitBytes)

		b, readErr := io.ReadAll(resp.Body)
		return resp, b, readErr
	}

	resp, body, err := doFetch(userAgent)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}

	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return ErrorResult(
				fmt.Sprintf(
					"failed to read response: size exceeded %d bytes limit",
					t.fetchLimitBytes,
				),
			)
		}
		return ErrorResult(err.Error())
	}

	// Cloudflare (and similar WAFs) signal bot challenges with 403 + cf-mitigated: challenge.
	// Retry once with an honest User-Agent that identifies ebiclaw, which some
	// operators explicitly allow-list for AI assistants.
	if resp.StatusCode == http.StatusForbidden && resp.Header.Get("Cf-Mitigated") == "challenge" {
		logger.DebugCF("tool", "Cloudflare challenge detected, retrying with honest User-Agent",
			map[string]any{"url": urlStr})
		honestUA := fmt.Sprintf(userAgentHonest, config.Version)
		resp2, body2, err2 := doFetch(honestUA)
		if resp2 != nil && resp2.Body != nil {
			defer resp2.Body.Close()
		}

		if err2 == nil {
			resp, body = resp2, body2
		} else {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err2, &maxBytesErr) {
				return ErrorResult(
					fmt.Sprintf("failed to read response: size exceeded %d bytes limit", t.fetchLimitBytes),
				)
			}
			return ErrorResult(err2.Error())
		}
	}

	bodyStr := string(body)
	contentType := resp.Header.Get("Content-Type")

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		// The most common error here is "mime: no media type" if the header is empty.
		logger.WarnCF("tool", "Failed to parse Content-Type", map[string]any{
			"raw_header": contentType,
			"error":      err.Error(),
		})

		// security fallback
		mediaType = "application/octet-stream"
	}

	charset, hasCharset := params["charset"]
	if hasCharset {
		// If the charset is not utf-8, we might have to convert the bodyStr
		// before passing it to the HTML/Markdown parser
		if strings.ToLower(charset) != "utf-8" {
			logger.WarnCF(
				"tool",
				"Note: the content is not in UTF-8",
				map[string]any{"charset": charset},
			)
		}
	}

	var text, extractor string

	switch {
	case mediaType == "application/json":
		var jsonData any
		if err := json.Unmarshal(body, &jsonData); err != nil {
			text = bodyStr
			extractor = "raw"
			break
		}

		formatted, err := json.MarshalIndent(jsonData, "", "  ")
		if err != nil {
			text = bodyStr
			extractor = "raw"
			break
		}

		text = string(formatted)
		extractor = "json"

	case mediaType == "text/html" || looksLikeHTML(bodyStr):
		switch strings.ToLower(t.format) {
		case "markdown":
			var err error
			text, err = utils.HtmlToMarkdown(bodyStr)
			if err != nil {
				return ErrorResult(fmt.Sprintf("failed to HTML to markdown: %v", err))
			}
			extractor = "markdown"

		default:
			text = t.extractText(bodyStr)
			extractor = "text"
		}

	default:
		text = bodyStr
		extractor = "raw"
	}

	truncated := len(text) > maxChars
	if truncated {
		text = text[:maxChars] + "\n[Content truncated due to size limit]"
	}

	result := map[string]any{
		"url":       urlStr,
		"status":    resp.StatusCode,
		"extractor": extractor,
		"truncated": truncated,
		"length":    len(text),
		"text":      text,
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")

	return &ToolResult{
		ForLLM: string(resultJSON),
		ForUser: fmt.Sprintf(
			"Fetched %d bytes from %s (extractor: %s, truncated: %v)",
			len(text),
			urlStr,
			extractor,
			truncated,
		),
	}
}

func looksLikeHTML(body string) bool {
	if body == "" {
		return false
	}

	lower := strings.ToLower(body)

	return strings.HasPrefix(body, "<!doctype") ||
		strings.HasPrefix(lower, "<html")
}

func (t *WebFetchTool) extractText(htmlContent string) string {
	result := reScript.ReplaceAllLiteralString(htmlContent, "")
	result = reStyle.ReplaceAllLiteralString(result, "")
	result = reTags.ReplaceAllLiteralString(result, "")

	result = strings.TrimSpace(result)

	result = reWhitespace.ReplaceAllString(result, " ")
	result = reBlankLines.ReplaceAllString(result, "\n\n")

	lines := strings.Split(result, "\n")
	var cleanLines []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleanLines = append(cleanLines, line)
		}
	}

	return strings.Join(cleanLines, "\n")
}

// newSafeDialContext re-resolves DNS at connect time to mitigate DNS rebinding (TOCTOU)
// where a hostname resolves to a public IP during pre-flight but a private IP at connect time.
func newSafeDialContext(
	dialer *net.Dialer,
	whitelist *privateHostWhitelist,
) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		if allowPrivateWebFetchHosts.Load() {
			return dialer.DialContext(ctx, network, address)
		}

		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid target address %q: %w", address, err)
		}
		if host == "" {
			return nil, fmt.Errorf("empty target host")
		}

		if ip := net.ParseIP(host); ip != nil {
			if shouldBlockPrivateIP(ip, whitelist) {
				return nil, fmt.Errorf("blocked private or local target: %s", host)
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		}

		ipAddrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve %s: %w", host, err)
		}

		attempted := 0
		var lastErr error
		for _, ipAddr := range ipAddrs {
			if shouldBlockPrivateIP(ipAddr.IP, whitelist) {
				continue
			}
			attempted++
			conn, err := dialer.DialContext(
				ctx,
				network,
				net.JoinHostPort(ipAddr.IP.String(), port),
			)
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}

		if attempted == 0 {
			return nil, fmt.Errorf(
				"all resolved addresses for %s are private, restricted, or not whitelisted",
				host,
			)
		}
		if lastErr != nil {
			return nil, fmt.Errorf(
				"failed connecting to public addresses for %s: %w",
				host,
				lastErr,
			)
		}
		return nil, fmt.Errorf("failed connecting to public addresses for %s", host)
	}
}

func newPrivateHostWhitelist(entries []string) (*privateHostWhitelist, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	whitelist := &privateHostWhitelist{
		exact: make(map[string]struct{}),
		cidrs: make([]*net.IPNet, 0, len(entries)),
	}
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if ip := net.ParseIP(entry); ip != nil {
			whitelist.exact[normalizeWhitelistIP(ip).String()] = struct{}{}
			continue
		}
		_, network, err := net.ParseCIDR(entry)
		if err != nil {
			return nil, fmt.Errorf("invalid entry %q: expected IP or CIDR", entry)
		}
		whitelist.cidrs = append(whitelist.cidrs, network)
	}

	if len(whitelist.exact) == 0 && len(whitelist.cidrs) == 0 {
		return nil, nil
	}
	return whitelist, nil
}

func (w *privateHostWhitelist) Contains(ip net.IP) bool {
	if w == nil || ip == nil {
		return false
	}

	normalized := normalizeWhitelistIP(ip)
	if _, ok := w.exact[normalized.String()]; ok {
		return true
	}
	for _, network := range w.cidrs {
		if network.Contains(normalized) {
			return true
		}
	}
	return false
}

func normalizeWhitelistIP(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}
	if ip4 := ip.To4(); ip4 != nil {
		return ip4
	}
	return ip
}

func shouldBlockPrivateIP(ip net.IP, whitelist *privateHostWhitelist) bool {
	return isPrivateOrRestrictedIP(ip) && !whitelist.Contains(ip)
}

// isObviousPrivateHost performs a lightweight, no-DNS check for obviously private hosts.
// It catches localhost, literal private IPs, and empty hosts. It does NOT resolve DNS —
// the real SSRF guard is newSafeDialContext which checks IPs at connect time.
func isObviousPrivateHost(host string, whitelist *privateHostWhitelist) bool {
	if allowPrivateWebFetchHosts.Load() {
		return false
	}

	h := strings.ToLower(strings.TrimSpace(host))
	h = strings.TrimSuffix(h, ".")
	if h == "" {
		return true
	}

	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}

	if ip := net.ParseIP(h); ip != nil {
		return shouldBlockPrivateIP(ip, whitelist)
	}

	return false
}

// isPrivateOrRestrictedIP returns true for IPs that should never be reached via web_fetch:
// RFC 1918, loopback, link-local (incl. cloud metadata 169.254.x.x), carrier-grade NAT,
// IPv6 unique-local (fc00::/7), 6to4 (2002::/16), and Teredo (2001:0000::/32).
func isPrivateOrRestrictedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}

	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}

	if ip4 := ip.To4(); ip4 != nil {
		// IPv4 private, loopback, link-local, and carrier-grade NAT ranges.
		if ip4[0] == 10 ||
			ip4[0] == 127 ||
			ip4[0] == 0 ||
			(ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31) ||
			(ip4[0] == 192 && ip4[1] == 168) ||
			(ip4[0] == 169 && ip4[1] == 254) ||
			(ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127) {
			return true
		}
		return false
	}

	if len(ip) == net.IPv6len {
		// IPv6 unique local addresses (fc00::/7)
		if (ip[0] & 0xfe) == 0xfc {
			return true
		}
		// 6to4 addresses (2002::/16): check the embedded IPv4 at bytes [2:6].
		if ip[0] == 0x20 && ip[1] == 0x02 {
			embedded := net.IPv4(ip[2], ip[3], ip[4], ip[5])
			return isPrivateOrRestrictedIP(embedded)
		}
		// Teredo (2001:0000::/32): client IPv4 is at bytes [12:16], XOR-inverted.
		if ip[0] == 0x20 && ip[1] == 0x01 && ip[2] == 0x00 && ip[3] == 0x00 {
			client := net.IPv4(ip[12]^0xff, ip[13]^0xff, ip[14]^0xff, ip[15]^0xff)
			return isPrivateOrRestrictedIP(client)
		}
	}

	return false
}
