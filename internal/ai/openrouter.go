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
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// APIError is an error response from OpenRouter (or the model provider behind it).
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("openrouter %d: %.300s", e.Status, e.Message) }

// ErrNotConfigured is returned when no API key is set.
var ErrNotConfigured = errors.New("ai assistant is not configured")

type Message struct {
	Role    string `json:"role"` // system | user | assistant
	Content string `json:"content"`
}

type Client struct {
	APIKey    string
	Model     string
	Fallbacks []string // tried in order when Model (or an earlier fallback) fails
	Reasoning bool     // ask the model to think before answering (slower, usually better)
	URL       string   // overridable for tests
	HTTP      *http.Client
	MaxTokens int
	Log       *slog.Logger // optional: says when a backup model had to answer

	// AttemptTimeout bounds each model except the last one tried, so a hung model can't eat the whole wait.
	AttemptTimeout time.Duration

	mu    sync.Mutex
	until map[string]time.Time // model -> don't try it first until then (it just failed)
}

func New(apiKey, model string, reasoning bool) *Client {
	return &Client{
		APIKey: apiKey, Model: model, Reasoning: reasoning,
		URL:       "https://openrouter.ai/api/v1/chat/completions",
		HTTP:      &http.Client{Timeout: 75 * time.Second},
		MaxTokens: 1500, // reasoning tokens count against this, so leave room for the answer

		AttemptTimeout: 35 * time.Second,
	}
}

// WithFallback adds a backup model, used only when the main one fails. Empty or repeated names are ignored.
func (c *Client) WithFallback(models ...string) *Client {
	for _, m := range models {
		m = strings.TrimSpace(m)
		if m == "" || m == c.Model {
			continue
		}
		dup := false
		for _, f := range c.Fallbacks {
			if f == m {
				dup = true
			}
		}
		if !dup {
			c.Fallbacks = append(c.Fallbacks, m)
		}
	}
	return c
}

func (c *Client) Configured() bool { return c != nil && c.APIKey != "" && c.Model != "" }

// Chat sends the conversation and returns the assistant's reply text. Any
// "reasoning" the model returns is deliberately discarded — users only see the answer.
//
// With backup models set, a failure (error, rate limit, timeout, empty answer) moves on to the next model,
// so one model going down or hitting its limit doesn't take the assistant with it. A model that just failed
// is tried last for a while, so people aren't kept waiting on it every time.
func (c *Client) Chat(ctx context.Context, msgs []Message) (string, error) {
	if !c.Configured() {
		return "", ErrNotConfigured
	}
	order := c.order()
	var lastErr error
	allBadRequest := true
	for i, model := range order {
		attemptCtx, cancel := ctx, context.CancelFunc(func() {})
		if i < len(order)-1 && c.AttemptTimeout > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, c.AttemptTimeout)
		}
		reply, err := c.chatOnce(attemptCtx, model, msgs)
		cancel()
		if err == nil {
			if i > 0 && c.Log != nil {
				c.Log.Warn("ai backup model answered", "model", model, "after", order[:i])
			}
			return reply, nil
		}
		lastErr = err
		if ctx.Err() != nil { // the caller gave up or ran out of time: more attempts can't help
			return "", err
		}
		var apiErr *APIError
		if !(errors.As(err, &apiErr) && apiErr.Status == http.StatusBadRequest) {
			allBadRequest = false
		}
		c.markFailed(model, err)
		if c.Log != nil && i < len(order)-1 {
			c.Log.Warn("ai model failed, trying the next one", "model", model, "err", err)
		}
	}
	// If every model refused the text itself (HTTP 400), keep that signal: the caller treats it specially.
	var apiErr *APIError
	if allBadRequest && errors.As(lastErr, &apiErr) {
		return "", apiErr
	}
	return "", lastErr
}

// order is the models to try: the healthy ones in their set order, then any that failed recently.
func (c *Client) order() []string {
	all := append([]string{c.Model}, c.Fallbacks...)
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	var healthy, resting []string
	for _, m := range all {
		if t, ok := c.until[m]; ok && now.Before(t) {
			resting = append(resting, m)
		} else {
			healthy = append(healthy, m)
		}
	}
	return append(healthy, resting...)
}

// markFailed benches a model for a while. A refusal of the text itself (400) says nothing about the model.
func (c *Client) markFailed(model string, err error) {
	rest := time.Minute
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case http.StatusBadRequest:
			return
		case http.StatusTooManyRequests, http.StatusPaymentRequired, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusGone:
			rest = 10 * time.Minute // a limit, a bad key or a retired model won't fix itself in a minute
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.until == nil {
		c.until = map[string]time.Time{}
	}
	c.until[model] = time.Now().Add(rest)
}

// chatOnce asks one model.
func (c *Client) chatOnce(ctx context.Context, model string, msgs []Message) (string, error) {
	body := map[string]any{
		"model": model, "messages": msgs, "max_tokens": c.MaxTokens, "temperature": 0.4,
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
		return "", &APIError{Status: resp.StatusCode, Message: msg}
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
