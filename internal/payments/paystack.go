// Package payments talks to the card processor (Paystack). Cribby never sees card details: the buyer pays on
// Paystack's own page, and Paystack tells us (by webhook, and when we ask) whether it worked.
package payments

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Charge is what the processor says about one payment.
type Charge struct {
	Paid       bool
	AmountKobo int
}

type InitArgs struct {
	Email       string
	AmountKobo  int
	Reference   string
	CallbackURL string
}

// Provider is the processor. Tests substitute a fake; with no key set, Configured is false and payments stay off.
type Provider interface {
	Configured() bool
	// Initialize starts a payment and returns the page the buyer pays on.
	Initialize(ctx context.Context, a InitArgs) (payURL string, err error)
	// Verify asks the processor what happened to a payment. The server trusts this, never the app.
	Verify(ctx context.Context, reference string) (Charge, error)
	// Refund returns a payment to the buyer in full.
	Refund(ctx context.Context, reference string, amountKobo int) error
	// ValidWebhook reports whether a webhook body really came from the processor.
	ValidWebhook(body []byte, signature string) bool
}

type Paystack struct {
	SecretKey string
	BaseURL   string // overridable for tests
	Client    *http.Client
}

func NewPaystack(secretKey string) *Paystack {
	return &Paystack{SecretKey: strings.TrimSpace(secretKey), BaseURL: "https://api.paystack.co", Client: &http.Client{Timeout: 30 * time.Second}}
}

func (p *Paystack) Configured() bool { return p != nil && p.SecretKey != "" }

func (p *Paystack) call(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(p.BaseURL, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.SecretKey)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var env struct {
		Status  bool            `json:"status"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(raw, &env)
	if resp.StatusCode >= 300 || !env.Status {
		msg := env.Message
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return errors.New("paystack: " + msg)
	}
	if out != nil {
		return json.Unmarshal(env.Data, out)
	}
	return nil
}

func (p *Paystack) Initialize(ctx context.Context, a InitArgs) (string, error) {
	var out struct {
		AuthorizationURL string `json:"authorization_url"`
	}
	err := p.call(ctx, http.MethodPost, "/transaction/initialize", map[string]any{
		"email": a.Email, "amount": a.AmountKobo, "reference": a.Reference, "callback_url": a.CallbackURL,
		"currency": "NGN",
	}, &out)
	if err == nil && out.AuthorizationURL == "" {
		err = errors.New("paystack: no payment page returned")
	}
	return out.AuthorizationURL, err
}

func (p *Paystack) Verify(ctx context.Context, reference string) (Charge, error) {
	var out struct {
		Status string `json:"status"`
		Amount int    `json:"amount"`
	}
	if err := p.call(ctx, http.MethodGet, "/transaction/verify/"+reference, nil, &out); err != nil {
		return Charge{}, err
	}
	return Charge{Paid: out.Status == "success", AmountKobo: out.Amount}, nil
}

func (p *Paystack) Refund(ctx context.Context, reference string, amountKobo int) error {
	return p.call(ctx, http.MethodPost, "/refund", map[string]any{"transaction": reference, "amount": amountKobo}, nil)
}

// ValidWebhook checks the x-paystack-signature header: an HMAC-SHA512 of the raw body, keyed with the secret key.
func (p *Paystack) ValidWebhook(body []byte, signature string) bool {
	if !p.Configured() || signature == "" {
		return false
	}
	mac := hmac.New(sha512.New, []byte(p.SecretKey))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(strings.ToLower(signature)))
}
