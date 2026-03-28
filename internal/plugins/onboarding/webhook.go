package onboarding

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// WebhookClient calls the API owner's provisioning webhook.
type WebhookClient struct {
	URL     string
	Secret  string // HMAC-SHA256 secret; empty = no signature
	Timeout time.Duration
	Client  *http.Client // injectable for testing
}

// NewWebhookClient creates a webhook client with the given config.
func NewWebhookClient(url, secret string, timeout time.Duration) *WebhookClient {
	return &WebhookClient{
		URL:     url,
		Secret:  secret,
		Timeout: timeout,
		Client: &http.Client{
			Timeout: timeout,
		},
	}
}

// Call sends a WebhookRequest to the provisioning endpoint and returns the response.
func (wc *WebhookClient) Call(req *WebhookRequest) (*WebhookResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal webhook request: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, wc.URL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create webhook request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	if wc.Secret != "" {
		sig := Sign(body, wc.Secret)
		httpReq.Header.Set("X-Webhook-Signature", "sha256="+sig)
	}

	resp, err := wc.Client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("webhook call failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return nil, fmt.Errorf("read webhook response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var webhookResp WebhookResponse
	if err := json.Unmarshal(respBody, &webhookResp); err != nil {
		return nil, fmt.Errorf("unmarshal webhook response: %w", err)
	}

	return &webhookResp, nil
}

// Sign computes HMAC-SHA256 of body using the given secret.
func Sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature checks that a signature header matches the expected HMAC-SHA256.
func VerifySignature(body []byte, secret, signature string) bool {
	expected := Sign(body, secret)
	return hmac.Equal([]byte("sha256="+expected), []byte(signature))
}
