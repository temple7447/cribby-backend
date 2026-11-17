package payments

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPaystackCalls(t *testing.T) {
	var gotAuth, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		gotBody = nil
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		switch r.URL.Path {
		case "/transaction/initialize":
			w.Write([]byte(`{"status":true,"data":{"authorization_url":"https://checkout.paystack.test/abc"}}`))
		case "/transaction/verify/ref1":
			w.Write([]byte(`{"status":true,"data":{"status":"success","amount":500000}}`))
		case "/refund":
			w.Write([]byte(`{"status":true,"data":{}}`))
		default:
			w.WriteHeader(400)
			w.Write([]byte(`{"status":false,"message":"nope"}`))
		}
	}))
	defer srv.Close()
	p := NewPaystack("sk_test_x")
	p.BaseURL = srv.URL
	ctx := context.Background()

	url, err := p.Initialize(ctx, InitArgs{Email: "a@b.com", AmountKobo: 500000, Reference: "ref1", CallbackURL: "https://x"})
	if err != nil || url != "https://checkout.paystack.test/abc" || gotAuth != "Bearer sk_test_x" || gotBody["amount"].(float64) != 500000 {
		t.Fatalf("initialize: %q %v %q %v", url, err, gotAuth, gotBody)
	}
	c, err := p.Verify(ctx, "ref1")
	if err != nil || !c.Paid || c.AmountKobo != 500000 || gotPath != "/transaction/verify/ref1" {
		t.Fatalf("verify: %+v %v", c, err)
	}
	if err := p.Refund(ctx, "ref1", 500000); err != nil || gotBody["transaction"] != "ref1" {
		t.Fatalf("refund: %v %v", err, gotBody)
	}
	if _, err := p.Verify(ctx, "missing"); err == nil {
		t.Fatal("a failure from Paystack must be an error")
	}
}

func TestWebhookSignature(t *testing.T) {
	p := NewPaystack("sk_test_x")
	body := []byte(`{"event":"charge.success"}`)
	mac := hmac.New(sha512.New, []byte("sk_test_x"))
	mac.Write(body)
	good := hex.EncodeToString(mac.Sum(nil))
	if !p.ValidWebhook(body, good) {
		t.Fatal("a correct signature must pass")
	}
	if p.ValidWebhook([]byte(`{"event":"charge.success","x":1}`), good) || p.ValidWebhook(body, "") || p.ValidWebhook(body, "deadbeef") {
		t.Fatal("a wrong or missing signature must fail")
	}
	if NewPaystack("").ValidWebhook(body, good) {
		t.Fatal("with no key nothing is valid")
	}
}
