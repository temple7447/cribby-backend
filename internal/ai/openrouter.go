// Package ai is a minimal OpenRouter client (https://openrouter.ai), used by the
// Cribby AI concierge. The API key lives only on the server.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNotConfigured is returned when no API key is set.
var ErrNotConfigured = errors.New("ai assistant is not configured")

type Message struct {
	Role    string `json:"role"` // system | user | assistant
	Content string `json:"content"`
}

type Client struct {
	APIKey    string
	Model     string
	Reasoning bool   // ask the model to think before answering (slower, usually better)
	URL       string // overridable for tests
	HTTP      *http.Client
	MaxTokens int
}

func New(apiKey, model string, reasoning bool) *Client {
	return &Client{
		APIKey: apiKey, Model: model, Reasoning: reasoning,
		URL:       "https://openrouter.ai/api/v1/chat/completions",
		HTTP:      &http.Client{Timeout: 75 * time.Second},
		MaxTokens: 1500, // reasoning tokens count against this, so leave room for the answer
	}
}

func (c *Client) Configured() bool { return c != nil && c.APIKey != "" && c.Model != "" }

// Chat sends the conversation and returns the assistant's reply text. Any
// "reasoning" the model returns is deliberately discarded — users only see the answer.
func (c *Client) Chat(ctx context.Context, msgs []Message) (string, error) {
	if !c.Configured() {
		return "", ErrNotConfigured
	}
	body := map[string]any{
		"model": c.Model, "messages": msgs, "max_tokens": c.MaxTokens, "temperature": 0.4,
	}
	if c.Reasoning {
		body["reasoning"] = map[string]any{"enabled": true}
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	// Optional attribution headers OpenRouter uses for app rankings.
	req.Header.Set("HTTP-Referer", "https://cribby.space")
	req.Header.Set("X-Title", "Cribby")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode >= 300 || out.Error != nil {
		msg := strings.TrimSpace(string(raw))
		if out.Error != nil && out.Error.Message != "" {
			msg = out.Error.Message
		}
		return "", fmt.Errorf("openrouter %s: %.300s", resp.Status, msg)
	}
	if len(out.Choices) == 0 {
		return "", errors.New("openrouter: no choices in response")
	}
	reply := strings.TrimSpace(out.Choices[0].Message.Content)
	if reply == "" {
		return "", errors.New("openrouter: empty reply (model may have run out of tokens while reasoning)")
	}
	return reply, nil
}
