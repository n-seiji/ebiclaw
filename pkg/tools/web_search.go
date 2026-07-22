package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/n-seiji/ebiclaw/pkg/utils"
)

type WebSearchTool struct {
	provider   SearchProvider
	maxResults int
}

type WebSearchToolOptions struct {
	BraveAPIKeys          []string
	BraveMaxResults       int
	BraveEnabled          bool
	TavilyAPIKeys         []string
	TavilyBaseURL         string
	TavilyMaxResults      int
	TavilyEnabled         bool
	DuckDuckGoMaxResults  int
	DuckDuckGoEnabled     bool
	PerplexityAPIKeys     []string
	PerplexityMaxResults  int
	PerplexityEnabled     bool
	SearXNGBaseURL        string
	SearXNGMaxResults     int
	SearXNGEnabled        bool
	GLMSearchAPIKey       string
	GLMSearchBaseURL      string
	GLMSearchEngine       string
	GLMSearchMaxResults   int
	GLMSearchEnabled      bool
	BaiduSearchAPIKey     string
	BaiduSearchBaseURL    string
	BaiduSearchMaxResults int
	BaiduSearchEnabled    bool
	Proxy                 string
}

func NewWebSearchTool(opts WebSearchToolOptions) (*WebSearchTool, error) {
	var provider SearchProvider
	maxResults := 10
	// Priority: Perplexity > Brave > SearXNG > Tavily > DuckDuckGo > Baidu Search > GLM Search
	if opts.PerplexityEnabled && len(opts.PerplexityAPIKeys) > 0 {
		client, err := utils.CreateHTTPClient(opts.Proxy, perplexityTimeout)
		if err != nil {
			return nil, fmt.Errorf("failed to create HTTP client for Perplexity: %w", err)
		}
		provider = &PerplexitySearchProvider{
			keyPool: NewAPIKeyPool(opts.PerplexityAPIKeys),
			proxy:   opts.Proxy,
			client:  client,
		}
		if opts.PerplexityMaxResults > 0 {
			maxResults = min(opts.PerplexityMaxResults, 10)
		}
	} else if opts.BraveEnabled && len(opts.BraveAPIKeys) > 0 {
		client, err := utils.CreateHTTPClient(opts.Proxy, searchTimeout)
		if err != nil {
			return nil, fmt.Errorf("failed to create HTTP client for Brave: %w", err)
		}
		provider = &BraveSearchProvider{keyPool: NewAPIKeyPool(opts.BraveAPIKeys), proxy: opts.Proxy, client: client}
		if opts.BraveMaxResults > 0 {
			maxResults = min(opts.BraveMaxResults, 10)
		}
	} else if opts.SearXNGEnabled && opts.SearXNGBaseURL != "" {
		provider = &SearXNGSearchProvider{baseURL: opts.SearXNGBaseURL}
		if opts.SearXNGMaxResults > 0 {
			maxResults = min(opts.SearXNGMaxResults, 10)
		}
	} else if opts.TavilyEnabled && len(opts.TavilyAPIKeys) > 0 {
		client, err := utils.CreateHTTPClient(opts.Proxy, searchTimeout)
		if err != nil {
			return nil, fmt.Errorf("failed to create HTTP client for Tavily: %w", err)
		}
		provider = &TavilySearchProvider{
			keyPool: NewAPIKeyPool(opts.TavilyAPIKeys),
			baseURL: opts.TavilyBaseURL,
			proxy:   opts.Proxy,
			client:  client,
		}
		if opts.TavilyMaxResults > 0 {
			maxResults = min(opts.TavilyMaxResults, 10)
		}
	} else if opts.DuckDuckGoEnabled {
		client, err := utils.CreateHTTPClient(opts.Proxy, searchTimeout)
		if err != nil {
			return nil, fmt.Errorf("failed to create HTTP client for DuckDuckGo: %w", err)
		}
		provider = &DuckDuckGoSearchProvider{proxy: opts.Proxy, client: client}
		if opts.DuckDuckGoMaxResults > 0 {
			maxResults = min(opts.DuckDuckGoMaxResults, 10)
		}
	} else if opts.BaiduSearchEnabled && opts.BaiduSearchAPIKey != "" {
		client, err := utils.CreateHTTPClient(opts.Proxy, perplexityTimeout)
		if err != nil {
			return nil, fmt.Errorf("failed to create HTTP client for Baidu Search: %w", err)
		}
		provider = &BaiduSearchProvider{
			apiKey:  opts.BaiduSearchAPIKey,
			baseURL: opts.BaiduSearchBaseURL,
			proxy:   opts.Proxy,
			client:  client,
		}
		if opts.BaiduSearchMaxResults > 0 {
			maxResults = min(opts.BaiduSearchMaxResults, 10)
		}
	} else if opts.GLMSearchEnabled && opts.GLMSearchAPIKey != "" {
		client, err := utils.CreateHTTPClient(opts.Proxy, searchTimeout)
		if err != nil {
			return nil, fmt.Errorf("failed to create HTTP client for GLM Search: %w", err)
		}
		searchEngine := opts.GLMSearchEngine
		if searchEngine == "" {
			searchEngine = "search_std"
		}
		provider = &GLMSearchProvider{
			apiKey:       opts.GLMSearchAPIKey,
			baseURL:      opts.GLMSearchBaseURL,
			searchEngine: searchEngine,
			proxy:        opts.Proxy,
			client:       client,
		}
		if opts.GLMSearchMaxResults > 0 {
			maxResults = min(opts.GLMSearchMaxResults, 10)
		}
	} else {
		return nil, nil
	}

	return &WebSearchTool{
		provider:   provider,
		maxResults: maxResults,
	}, nil
}

func (t *WebSearchTool) Name() string {
	return "web_search"
}

func (t *WebSearchTool) Description() string {
	return "Search the web for current information. Supports query, count, and an optional temporal range filter. Returns titles, URLs, and snippets from search results."
}

func (t *WebSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query",
			},
			"count": map[string]any{
				"type":        "integer",
				"description": "Number of results (default: 10, max: 10)",
				"minimum":     1.0,
				"maximum":     10.0,
			},
			"range": map[string]any{
				"type":        "string",
				"description": "Optional time filter: d (day), w (week), m (month), y (year)",
				"enum":        []string{"d", "w", "m", "y"},
			},
		},
		"required": []string{"query"},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	query, ok := args["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return ErrorResult("query is required")
	}
	query = strings.TrimSpace(query)

	count64, err := getInt64Arg(args, "count", int64(t.maxResults))
	if err != nil {
		return ErrorResult(err.Error())
	}
	count := t.maxResults
	if count64 > 0 && count64 <= 10 {
		count = int(count64)
	}

	rangeCode, err := normalizeSearchRange("")
	if err != nil {
		return ErrorResult(err.Error())
	}
	if rawRange, exists := args["range"]; exists {
		rangeStr, ok := rawRange.(string)
		if !ok {
			return ErrorResult("range must be a string")
		}
		rangeCode, err = normalizeSearchRange(rangeStr)
		if err != nil {
			return ErrorResult(err.Error())
		}
	}

	result, err := t.provider.Search(ctx, query, count, rangeCode)
	if err != nil {
		return ErrorResult(fmt.Sprintf("search failed: %v", err))
	}

	return &ToolResult{
		ForLLM:  result,
		ForUser: result,
	}
}
