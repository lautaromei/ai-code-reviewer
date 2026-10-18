package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	defaultBaseURL          = "https://api.anthropic.com"
	defaultAnthropicVersion = "2023-06-01"
	defaultMaxTokens        = 4096
)

type Client struct {
	apiKey           string
	model            string
	promptVersion    string
	baseURL          string
	anthropicVersion string
	maxTokens        int
	effort           string
	httpClient       *http.Client
}

type Option func(*Client)

func WithBaseURL(url string) Option { return func(c *Client) { c.baseURL = url } }

func WithHTTPClient(hc *http.Client) Option { return func(c *Client) { c.httpClient = hc } }

func WithAnthropicVersion(v string) Option { return func(c *Client) { c.anthropicVersion = v } }

func WithMaxTokens(n int) Option {
	return func(c *Client) {
		if n > 0 {
			c.maxTokens = n
		}
	}
}

// WithEffort configures adaptive thinking depth for models that support the
// Messages API output_config.effort parameter.
func WithEffort(effort string) Option { return func(c *Client) { c.effort = effort } }

func New(apiKey string, model string, promptVersion string, opts ...Option) *Client {
	c := &Client{
		apiKey:           apiKey,
		model:            model,
		promptVersion:    promptVersion,
		baseURL:          defaultBaseURL,
		anthropicVersion: defaultAnthropicVersion,
		maxTokens:        defaultMaxTokens,
		httpClient:       &http.Client{Timeout: 60 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type toolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type messageParam struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messageRequest struct {
	Model        string          `json:"model"`
	MaxTokens    int             `json:"max_tokens"`
	System       any             `json:"system,omitempty"`
	Thinking     *thinkingParam  `json:"thinking,omitempty"`
	OutputConfig *outputConfig   `json:"output_config,omitempty"`
	Messages     []messageParam  `json:"messages"`
	Tools        []anthropicTool `json:"tools"`
	ToolChoice   toolChoice      `json:"tool_choice"`
}

type cacheControl struct {
	Type string `json:"type"`
}

type systemBlock struct {
	Type         string        `json:"type"`
	Text         string        `json:"text"`
	CacheControl *cacheControl `json:"cache_control,omitempty"`
}

type thinkingParam struct {
	Type string `json:"type"`
}

type outputConfig struct {
	Effort string `json:"effort"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type messageResponse struct {
	Model      string         `json:"model"`
	Content    []contentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
	Usage      messageUsage   `json:"usage"`
}

type messageUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

type anthropicErrorBody struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) callTool(ctx context.Context, systemPrompt string, userPrompt string, tool anthropicTool) (json.RawMessage, error) {
	raw, _, _, err := c.callToolWithUsage(ctx, systemPrompt, userPrompt, tool)
	return raw, err
}

func (c *Client) callToolWithUsage(ctx context.Context, systemPrompt string, userPrompt string, tool anthropicTool) (json.RawMessage, messageUsage, string, error) {
	return c.callToolWithSystem(ctx, systemPrompt, userPrompt, tool)
}

func (c *Client) callToolWithSystemBlocks(ctx context.Context, system []systemBlock, userPrompt string, tool anthropicTool) (json.RawMessage, messageUsage, string, error) {
	return c.callToolWithSystem(ctx, system, userPrompt, tool)
}

func (c *Client) callToolWithSystem(ctx context.Context, system any, userPrompt string, tool anthropicTool) (json.RawMessage, messageUsage, string, error) {
	if c.apiKey == "" {
		return nil, messageUsage{}, "", fmt.Errorf("anthropic: API key is missing")
	}
	if c.model == "" {
		return nil, messageUsage{}, "", fmt.Errorf("anthropic: model is missing (classifier.model)")
	}

	var thinking *thinkingParam
	var output *outputConfig
	if c.model == "claude-sonnet-5-5" {
		thinking = &thinkingParam{Type: "between_tools"}
	}
	if c.effort != "" {
		thinking = &thinkingParam{Type: "adaptive"}
		output = &outputConfig{Effort: c.effort}
	}
	reqBody := messageRequest{
		Model:        c.model,
		MaxTokens:    c.maxTokens,
		System:       system,
		Thinking:     thinking,
		OutputConfig: output,
		Messages:     []messageParam{{Role: "user", Content: userPrompt}},
		Tools:        []anthropicTool{tool},
		// Claude 5.x models only accept automatic tool selection for requests
		// that use their current reasoning mode. The prompt still asks for the
		// structured tool response, and the response parser rejects plain text.
		ToolChoice: toolChoice{Type: "auto"},
	}

	payload, _ := json.Marshal(reqBody)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return nil, messageUsage{}, "", fmt.Errorf("anthropic: build request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", c.anthropicVersion)

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, messageUsage{}, "", fmt.Errorf("anthropic: call to the Messages API failed: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, messageUsage{}, "", fmt.Errorf("anthropic: read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		var errBody anthropicErrorBody
		_ = json.Unmarshal(respBody, &errBody)
		if errBody.Error.Message != "" {
			return nil, messageUsage{}, "", fmt.Errorf("anthropic: %s (HTTP %d): %s", errBody.Error.Type, httpResp.StatusCode, errBody.Error.Message)
		}
		return nil, messageUsage{}, "", fmt.Errorf("anthropic: HTTP %d: %s", httpResp.StatusCode, string(respBody))
	}

	var resp messageResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, messageUsage{}, "", fmt.Errorf("anthropic: invalid response: %w", err)
	}

	for _, block := range resp.Content {
		if block.Type == "tool_use" && block.Name == tool.Name {
			return block.Input, resp.Usage, resp.Model, nil
		}
	}
	return nil, resp.Usage, resp.Model, fmt.Errorf("anthropic: response did not invoke tool %q (stop_reason=%q)", tool.Name, resp.StopReason)
}
