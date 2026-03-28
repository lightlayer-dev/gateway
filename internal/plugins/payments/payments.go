// Package payments implements the x402 payment protocol plugin.
// Ported from agent-layer-ts x402.ts and x402-handler.ts.
package payments

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/lightlayer-dev/gateway/internal/plugins"
)

func init() {
	plugins.Register("payments", func() plugins.Plugin { return New() })
}

// ── Constants ────────────────────────────────────────────────────────────

const (
	X402Version           = 1
	HeaderPaymentRequired = "Payment-Required"
	HeaderPaymentSig      = "Payment-Signature"
	HeaderPaymentResponse = "Payment-Response"

	defaultScheme          = "exact"
	defaultMaxTimeout      = 60
	defaultNetwork         = "eip155:8453"
	defaultCurrency        = "USDC"
)

// ── x402 Types ──────────────────────────────────────────────────────────

// PaymentRequirements describes what the server requires for payment.
type PaymentRequirements struct {
	Scheme            string                 `json:"scheme"`
	Network           string                 `json:"network"`
	Asset             string                 `json:"asset"`
	Amount            string                 `json:"amount"`
	PayTo             string                 `json:"payTo"`
	MaxTimeoutSeconds int                    `json:"maxTimeoutSeconds"`
	Extra             map[string]interface{} `json:"extra"`
}

// PaymentRequired is the 402 response body / header payload.
type PaymentRequired struct {
	X402Version int                   `json:"x402Version"`
	Error       string                `json:"error,omitempty"`
	Resource    ResourceInfo          `json:"resource"`
	Accepts     []PaymentRequirements `json:"accepts"`
}

// ResourceInfo describes the resource being paid for.
type ResourceInfo struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// PaymentPayload is what the client sends back after paying.
type PaymentPayload struct {
	X402Version int                    `json:"x402Version"`
	Resource    *ResourceInfo          `json:"resource,omitempty"`
	Accepted    PaymentRequirements    `json:"accepted"`
	Payload     map[string]interface{} `json:"payload"`
}

// VerifyResponse is the facilitator verify response.
type VerifyResponse struct {
	IsValid       bool   `json:"isValid"`
	InvalidReason string `json:"invalidReason,omitempty"`
}

// SettleResponse is the facilitator settle response.
type SettleResponse struct {
	Success     bool   `json:"success"`
	TxHash      string `json:"txHash,omitempty"`
	Network     string `json:"network,omitempty"`
	ErrorReason string `json:"errorReason,omitempty"`
}

// ── Billing webhook ─────────────────────────────────────────────────────

// BillingWebhookRequest is sent to the origin's billing endpoint after a successful x402 payment.
type BillingWebhookRequest struct {
	AgentID   string `json:"agent_id"`
	Amount    string `json:"amount"`
	Currency  string `json:"currency"`
	TxHash    string `json:"tx_hash"`
	Network   string `json:"network"`
	Timestamp string `json:"timestamp"` // RFC 3339
}

// BillingWebhookClient calls the origin's billing webhook.
type BillingWebhookClient struct {
	URL     string
	Secret  string // HMAC-SHA256 secret; empty = no signature
	Timeout time.Duration
	Client  *http.Client
}

// NewBillingWebhookClient creates a billing webhook client.
func NewBillingWebhookClient(url, secret string, timeout time.Duration) *BillingWebhookClient {
	return &BillingWebhookClient{
		URL:     url,
		Secret:  secret,
		Timeout: timeout,
		Client: &http.Client{
			Timeout: timeout,
		},
	}
}

// Call sends payment details to the origin's billing webhook.
func (bw *BillingWebhookClient) Call(req *BillingWebhookRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal billing webhook request: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, bw.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create billing webhook request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	if bw.Secret != "" {
		sig := billingSign(body, bw.Secret)
		httpReq.Header.Set("X-Webhook-Signature", "sha256="+sig)
	}

	resp, err := bw.Client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("billing webhook call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("billing webhook returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// billingSign computes HMAC-SHA256 of body using the given secret.
func billingSign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// ── Route config ────────────────────────────────────────────────────────

// routeConfig holds parsed per-route payment configuration.
type routeConfig struct {
	path              string
	payTo             string
	scheme            string
	amount            string
	asset             string
	network           string
	maxTimeoutSeconds int
	description       string
}

// ── Facilitator client ──────────────────────────────────────────────────

// FacilitatorClient communicates with an x402 facilitator.
type FacilitatorClient interface {
	Verify(payload *PaymentPayload, requirements *PaymentRequirements) (*VerifyResponse, error)
	Settle(payload *PaymentPayload, requirements *PaymentRequirements) (*SettleResponse, error)
}

// httpFacilitator is the default HTTP-based facilitator client.
type httpFacilitator struct {
	url    string
	client *http.Client
}

func newHTTPFacilitator(url string) *httpFacilitator {
	return &httpFacilitator{
		url: strings.TrimRight(url, "/"),
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (f *httpFacilitator) Verify(payload *PaymentPayload, requirements *PaymentRequirements) (*VerifyResponse, error) {
	body, err := json.Marshal(map[string]interface{}{
		"payload":      payload,
		"requirements": requirements,
	})
	if err != nil {
		return nil, fmt.Errorf("marshaling verify request: %w", err)
	}

	resp, err := f.client.Post(f.url+"/verify", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("facilitator verify request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("facilitator verify failed: %d %s", resp.StatusCode, string(respBody))
	}

	var result VerifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding verify response: %w", err)
	}
	return &result, nil
}

func (f *httpFacilitator) Settle(payload *PaymentPayload, requirements *PaymentRequirements) (*SettleResponse, error) {
	body, err := json.Marshal(map[string]interface{}{
		"payload":      payload,
		"requirements": requirements,
	})
	if err != nil {
		return nil, fmt.Errorf("marshaling settle request: %w", err)
	}

	resp, err := f.client.Post(f.url+"/settle", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("facilitator settle request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("facilitator settle failed: %d %s", resp.StatusCode, string(respBody))
	}

	var result SettleResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding settle response: %w", err)
	}
	return &result, nil
}

// ── Plugin ──────────────────────────────────────────────────────────────

// Plugin implements the x402 payment flow as gateway middleware.
type Plugin struct {
	routes         []routeConfig
	facilitator    FacilitatorClient
	billingWebhook *BillingWebhookClient // nil if billing webhook not configured
}

// New creates a new payments plugin.
func New() *Plugin {
	return &Plugin{}
}

func (p *Plugin) Name() string { return "payments" }

func (p *Plugin) Init(cfg map[string]interface{}) error {
	if cfg == nil {
		return fmt.Errorf("payments plugin requires configuration")
	}

	facilitatorURL, _ := cfg["facilitator"].(string)
	if facilitatorURL == "" {
		return fmt.Errorf("payments plugin requires facilitator URL")
	}
	p.facilitator = newHTTPFacilitator(facilitatorURL)

	globalPayTo, _ := cfg["pay_to"].(string)
	globalNetwork, _ := cfg["network"].(string)
	globalScheme, _ := cfg["scheme"].(string)

	routes, _ := cfg["routes"].([]map[string]interface{})
	for _, r := range routes {
		path, _ := r["path"].(string)
		price, _ := r["price"].(string)
		if path == "" || price == "" {
			continue
		}

		rc := routeConfig{
			path:   path,
			amount: price,
			asset:  defaultCurrency,
		}

		if currency, ok := r["currency"].(string); ok && currency != "" {
			rc.asset = currency
		}
		if payTo, ok := r["pay_to"].(string); ok && payTo != "" {
			rc.payTo = payTo
		} else {
			rc.payTo = globalPayTo
		}
		if network, ok := r["network"].(string); ok && network != "" {
			rc.network = network
		} else if globalNetwork != "" {
			rc.network = globalNetwork
		} else {
			rc.network = defaultNetwork
		}
		if scheme, ok := r["scheme"].(string); ok && scheme != "" {
			rc.scheme = scheme
		} else if globalScheme != "" {
			rc.scheme = globalScheme
		} else {
			rc.scheme = defaultScheme
		}
		if maxTimeout, ok := r["max_timeout_seconds"].(int); ok && maxTimeout > 0 {
			rc.maxTimeoutSeconds = maxTimeout
		} else {
			rc.maxTimeoutSeconds = defaultMaxTimeout
		}
		if desc, ok := r["description"].(string); ok {
			rc.description = desc
		}

		p.routes = append(p.routes, rc)
	}

	// Parse billing webhook config.
	if billingURL, _ := cfg["billing_webhook"].(string); billingURL != "" {
		billingSecret, _ := cfg["billing_webhook_secret"].(string)
		billingTimeout := 10 * time.Second
		if t, ok := cfg["billing_webhook_timeout"].(string); ok && t != "" {
			if parsed, err := time.ParseDuration(t); err == nil {
				billingTimeout = parsed
			}
		}
		p.billingWebhook = NewBillingWebhookClient(billingURL, billingSecret, billingTimeout)
		slog.Info("payments billing webhook configured", "url", billingURL)
	}

	slog.Info("payments plugin initialized",
		"facilitator", facilitatorURL,
		"paid_routes", len(p.routes),
	)
	return nil
}

func (p *Plugin) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rc := p.matchRoute(r.Method, r.URL.Path)
			if rc == nil {
				// No paid route configured — but if billing webhook is set,
				// intercept 429 from origin and convert to 402.
				if p.billingWebhook != nil && len(p.routes) > 0 {
					rw := &statusInterceptWriter{ResponseWriter: w}
					next.ServeHTTP(rw, r)
					if rw.statusCode == http.StatusTooManyRequests {
						// Origin returned 429 (quota exceeded) — convert to 402 with
						// payment info from the first configured route as default pricing.
						defaultRC := &p.routes[0]
						reqURL := requestURL(r)
						pr := buildPaymentRequired(reqURL, defaultRC, "Quota exceeded — payment required to continue")
						p.write402(w, pr)
					}
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			reqURL := requestURL(r)
			requirements := buildRequirements(rc)

			// Check for payment signature header.
			sigHeader := r.Header.Get(HeaderPaymentSig)
			if sigHeader == "" {
				// No payment — return 402.
				pr := buildPaymentRequired(reqURL, rc, "")
				p.write402(w, pr)
				return
			}

			// Decode the payment payload.
			payload, err := decodePaymentPayload(sigHeader)
			if err != nil {
				pr := buildPaymentRequired(reqURL, rc, "Invalid payment signature format")
				p.write402(w, pr)
				return
			}

			// Verify with facilitator.
			verifyResult, err := p.facilitator.Verify(payload, requirements)
			if err != nil {
				slog.Error("facilitator verify error", "error", err)
				plugins.WriteError(w, http.StatusBadGateway, "payment_verification_failed",
					"Could not verify payment with facilitator")
				return
			}

			if !verifyResult.IsValid {
				reason := verifyResult.InvalidReason
				if reason == "" {
					reason = "Payment verification failed"
				}
				pr := buildPaymentRequired(reqURL, rc, reason)
				p.write402(w, pr)
				return
			}

			// Settle with facilitator.
			settleResult, err := p.facilitator.Settle(payload, requirements)
			if err != nil {
				slog.Error("facilitator settle error", "error", err)
				plugins.WriteError(w, http.StatusBadGateway, "payment_settlement_failed",
					"Could not settle payment with facilitator")
				return
			}

			if !settleResult.Success {
				reason := settleResult.ErrorReason
				if reason == "" {
					reason = "Payment settlement failed"
				}
				pr := buildPaymentRequired(reqURL, rc, reason)
				p.write402(w, pr)
				return
			}

			// Payment successful — call billing webhook if configured.
			if p.billingWebhook != nil {
				agentID := r.Header.Get("X-Agent-ID")
				if agentID == "" {
					agentID = "unknown"
				}
				billingReq := &BillingWebhookRequest{
					AgentID:   agentID,
					Amount:    rc.amount,
					Currency:  rc.asset,
					TxHash:    settleResult.TxHash,
					Network:   settleResult.Network,
					Timestamp: time.Now().UTC().Format(time.RFC3339),
				}
				if err := p.billingWebhook.Call(billingReq); err != nil {
					slog.Error("billing webhook failed", "error", err)
					// Continue anyway — payment was already settled on-chain.
				}
			}

			// Set response header and forward.
			settlementJSON, _ := json.Marshal(settleResult)
			w.Header().Set(HeaderPaymentResponse, base64.StdEncoding.EncodeToString(settlementJSON))

			next.ServeHTTP(w, r)
		})
	}
}

func (p *Plugin) Close() error { return nil }

// ── Route matching ──────────────────────────────────────────────────────

// matchRoute finds the route config for a given method+path.
// Supports exact matches and wildcard patterns (path ending with /*).
// Exact matches take priority over wildcards; longest prefix wins among wildcards.
func (p *Plugin) matchRoute(method, path string) *routeConfig {
	upperMethod := strings.ToUpper(method)

	// Exact match first.
	for i := range p.routes {
		rc := &p.routes[i]
		pattern := rc.path
		if !strings.Contains(pattern, " ") {
			// Path-only pattern — match any method.
			if pattern == path {
				return rc
			}
			continue
		}
		parts := strings.SplitN(pattern, " ", 2)
		if strings.ToUpper(parts[0]) == upperMethod && parts[1] == path {
			return rc
		}
	}

	// Wildcard matching — longest prefix wins.
	var best *routeConfig
	bestLen := -1

	for i := range p.routes {
		rc := &p.routes[i]
		pattern := rc.path
		if !strings.HasSuffix(pattern, "/*") {
			continue
		}

		var patternMethod, patternPath string
		if strings.Contains(pattern, " ") {
			parts := strings.SplitN(pattern, " ", 2)
			patternMethod = strings.ToUpper(parts[0])
			patternPath = parts[1]
		} else {
			patternMethod = ""
			patternPath = pattern
		}

		if patternMethod != "" && patternMethod != upperMethod {
			continue
		}

		// "/api/*" → prefix is "/api/"
		prefix := patternPath[:len(patternPath)-1] // remove trailing "*"
		if strings.HasPrefix(path, prefix) && len(prefix) > bestLen {
			bestLen = len(prefix)
			best = rc
		}
	}

	return best
}

// ── Helpers ─────────────────────────────────────────────────────────────

func buildRequirements(rc *routeConfig) *PaymentRequirements {
	return &PaymentRequirements{
		Scheme:            rc.scheme,
		Network:           rc.network,
		Asset:             rc.asset,
		Amount:            rc.amount,
		PayTo:             rc.payTo,
		MaxTimeoutSeconds: rc.maxTimeoutSeconds,
		Extra:             map[string]interface{}{},
	}
}

func buildPaymentRequired(url string, rc *routeConfig, errMsg string) *PaymentRequired {
	return &PaymentRequired{
		X402Version: X402Version,
		Error:       errMsg,
		Resource: ResourceInfo{
			URL:         url,
			Description: rc.description,
		},
		Accepts: []PaymentRequirements{*buildRequirements(rc)},
	}
}

func encodePaymentRequired(pr *PaymentRequired) string {
	data, _ := json.Marshal(pr)
	return base64.StdEncoding.EncodeToString(data)
}

func decodePaymentPayload(header string) (*PaymentPayload, error) {
	data, err := base64.StdEncoding.DecodeString(header)
	if err != nil {
		return nil, fmt.Errorf("invalid base64: %w", err)
	}
	var payload PaymentPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return &payload, nil
}

func (p *Plugin) write402(w http.ResponseWriter, pr *PaymentRequired) {
	encoded := encodePaymentRequired(pr)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set(HeaderPaymentRequired, encoded)
	w.WriteHeader(http.StatusPaymentRequired)
	json.NewEncoder(w).Encode(pr)
}

// statusInterceptWriter captures the status code without writing the body,
// allowing the payments plugin to intercept 429 responses from the origin.
type statusInterceptWriter struct {
	http.ResponseWriter
	statusCode  int
	headersSent bool
}

func (w *statusInterceptWriter) WriteHeader(code int) {
	w.statusCode = code
	if code != http.StatusTooManyRequests {
		w.headersSent = true
		w.ResponseWriter.WriteHeader(code)
	}
}

func (w *statusInterceptWriter) Write(b []byte) (int, error) {
	if w.statusCode == http.StatusTooManyRequests {
		// Suppress the origin's 429 body — we'll replace it with 402.
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}

func requestURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s%s", scheme, r.Host, r.URL.RequestURI())
}
