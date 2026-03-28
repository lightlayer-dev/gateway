package payments

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockFacilitator is a test facilitator that returns configured responses.
type mockFacilitator struct {
	verifyResp  *VerifyResponse
	verifyErr   error
	settleResp  *SettleResponse
	settleErr   error
	verifyCalls int
	settleCalls int
}

func (m *mockFacilitator) Verify(payload *PaymentPayload, requirements *PaymentRequirements) (*VerifyResponse, error) {
	m.verifyCalls++
	return m.verifyResp, m.verifyErr
}

func (m *mockFacilitator) Settle(payload *PaymentPayload, requirements *PaymentRequirements) (*SettleResponse, error) {
	m.settleCalls++
	return m.settleResp, m.settleErr
}

func newTestPlugin(routes []routeConfig, facilitator FacilitatorClient) *Plugin {
	return &Plugin{
		routes:      routes,
		facilitator: facilitator,
	}
}

func makePaymentSignature(payload *PaymentPayload) string {
	data, _ := json.Marshal(payload)
	return base64.StdEncoding.EncodeToString(data)
}

var testRoutes = []routeConfig{
	{
		path:              "/api/premium/*",
		payTo:             "0xABC123",
		scheme:            "exact",
		amount:            "0.01",
		asset:             "USDC",
		network:           "eip155:8453",
		maxTimeoutSeconds: 60,
		description:       "Premium API access",
	},
	{
		path:              "GET /api/weather",
		payTo:             "0xABC123",
		scheme:            "exact",
		amount:            "0.001",
		asset:             "USDC",
		network:           "eip155:8453",
		maxTimeoutSeconds: 60,
		description:       "Weather data",
	},
}

func validPayload() *PaymentPayload {
	return &PaymentPayload{
		X402Version: 1,
		Accepted: PaymentRequirements{
			Scheme:            "exact",
			Network:           "eip155:8453",
			Asset:             "USDC",
			Amount:            "0.01",
			PayTo:             "0xABC123",
			MaxTimeoutSeconds: 60,
			Extra:             map[string]interface{}{},
		},
		Payload: map[string]interface{}{"signature": "0xdeadbeef"},
	}
}

// origin is a simple handler that returns 200 for forwarded requests.
var origin = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"ok":true}`))
})

func TestFreeRoutePassesThrough(t *testing.T) {
	p := newTestPlugin(testRoutes, &mockFacilitator{})
	handler := p.Middleware()(origin)

	req := httptest.NewRequest("GET", "/api/free/stuff", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"ok":true`)
}

func TestUnpaidRouteGets402(t *testing.T) {
	p := newTestPlugin(testRoutes, &mockFacilitator{})
	handler := p.Middleware()(origin)

	req := httptest.NewRequest("GET", "/api/premium/data", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusPaymentRequired, rec.Code)
	assert.NotEmpty(t, rec.Header().Get(HeaderPaymentRequired))
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body PaymentRequired
	err := json.Unmarshal(rec.Body.Bytes(), &body)
	require.NoError(t, err)
	assert.Equal(t, X402Version, body.X402Version)
	assert.Len(t, body.Accepts, 1)
	assert.Equal(t, "0.01", body.Accepts[0].Amount)
	assert.Equal(t, "USDC", body.Accepts[0].Asset)
	assert.Equal(t, "0xABC123", body.Accepts[0].PayTo)
}

func TestUnpaidExactRouteGets402(t *testing.T) {
	p := newTestPlugin(testRoutes, &mockFacilitator{})
	handler := p.Middleware()(origin)

	req := httptest.NewRequest("GET", "/api/weather", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusPaymentRequired, rec.Code)

	var body PaymentRequired
	err := json.Unmarshal(rec.Body.Bytes(), &body)
	require.NoError(t, err)
	assert.Equal(t, "0.001", body.Accepts[0].Amount)
	assert.Equal(t, "Weather data", body.Resource.Description)
}

func TestValidPaymentPassesThrough(t *testing.T) {
	fac := &mockFacilitator{
		verifyResp: &VerifyResponse{IsValid: true},
		settleResp: &SettleResponse{Success: true, TxHash: "0xTX123", Network: "eip155:8453"},
	}
	p := newTestPlugin(testRoutes, fac)
	handler := p.Middleware()(origin)

	req := httptest.NewRequest("GET", "/api/premium/data", nil)
	req.Header.Set(HeaderPaymentSig, makePaymentSignature(validPayload()))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"ok":true`)

	// Payment-Response header should be set.
	paymentResp := rec.Header().Get(HeaderPaymentResponse)
	assert.NotEmpty(t, paymentResp)

	// Decode and verify settlement response.
	decoded, err := base64.StdEncoding.DecodeString(paymentResp)
	require.NoError(t, err)
	var settle SettleResponse
	require.NoError(t, json.Unmarshal(decoded, &settle))
	assert.True(t, settle.Success)
	assert.Equal(t, "0xTX123", settle.TxHash)

	assert.Equal(t, 1, fac.verifyCalls)
	assert.Equal(t, 1, fac.settleCalls)
}

func TestInvalidSignatureFormatGets402(t *testing.T) {
	p := newTestPlugin(testRoutes, &mockFacilitator{})
	handler := p.Middleware()(origin)

	req := httptest.NewRequest("GET", "/api/premium/data", nil)
	req.Header.Set(HeaderPaymentSig, "not-valid-base64!!!")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusPaymentRequired, rec.Code)

	var body PaymentRequired
	err := json.Unmarshal(rec.Body.Bytes(), &body)
	require.NoError(t, err)
	assert.Equal(t, "Invalid payment signature format", body.Error)
}

func TestVerifyFailedGets402(t *testing.T) {
	fac := &mockFacilitator{
		verifyResp: &VerifyResponse{IsValid: false, InvalidReason: "Insufficient funds"},
	}
	p := newTestPlugin(testRoutes, fac)
	handler := p.Middleware()(origin)

	req := httptest.NewRequest("GET", "/api/premium/data", nil)
	req.Header.Set(HeaderPaymentSig, makePaymentSignature(validPayload()))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusPaymentRequired, rec.Code)

	var body PaymentRequired
	err := json.Unmarshal(rec.Body.Bytes(), &body)
	require.NoError(t, err)
	assert.Equal(t, "Insufficient funds", body.Error)
}

func TestVerifyErrorGets502(t *testing.T) {
	fac := &mockFacilitator{
		verifyErr: assert.AnError,
	}
	p := newTestPlugin(testRoutes, fac)
	handler := p.Middleware()(origin)

	req := httptest.NewRequest("GET", "/api/premium/data", nil)
	req.Header.Set(HeaderPaymentSig, makePaymentSignature(validPayload()))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestSettleFailedGets402(t *testing.T) {
	fac := &mockFacilitator{
		verifyResp: &VerifyResponse{IsValid: true},
		settleResp: &SettleResponse{Success: false, ErrorReason: "Network congestion"},
	}
	p := newTestPlugin(testRoutes, fac)
	handler := p.Middleware()(origin)

	req := httptest.NewRequest("GET", "/api/premium/data", nil)
	req.Header.Set(HeaderPaymentSig, makePaymentSignature(validPayload()))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusPaymentRequired, rec.Code)

	var body PaymentRequired
	err := json.Unmarshal(rec.Body.Bytes(), &body)
	require.NoError(t, err)
	assert.Equal(t, "Network congestion", body.Error)
}

func TestSettleErrorGets502(t *testing.T) {
	fac := &mockFacilitator{
		verifyResp: &VerifyResponse{IsValid: true},
		settleErr:  assert.AnError,
	}
	p := newTestPlugin(testRoutes, fac)
	handler := p.Middleware()(origin)

	req := httptest.NewRequest("GET", "/api/premium/data", nil)
	req.Header.Set(HeaderPaymentSig, makePaymentSignature(validPayload()))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestRouteMatchingWildcardPriority(t *testing.T) {
	routes := []routeConfig{
		{path: "/api/*", amount: "0.001", asset: "USDC", scheme: "exact", network: "eip155:8453", payTo: "0xA", maxTimeoutSeconds: 60},
		{path: "/api/premium/*", amount: "0.01", asset: "USDC", scheme: "exact", network: "eip155:8453", payTo: "0xB", maxTimeoutSeconds: 60},
	}
	p := newTestPlugin(routes, &mockFacilitator{})

	// /api/premium/x should match the more specific wildcard.
	rc := p.matchRoute("GET", "/api/premium/x")
	require.NotNil(t, rc)
	assert.Equal(t, "0xB", rc.payTo)

	// /api/other should match the general wildcard.
	rc = p.matchRoute("GET", "/api/other")
	require.NotNil(t, rc)
	assert.Equal(t, "0xA", rc.payTo)
}

func TestRouteMatchingMethodScoped(t *testing.T) {
	routes := []routeConfig{
		{path: "POST /api/data", amount: "0.01", asset: "USDC", scheme: "exact", network: "eip155:8453", payTo: "0xA", maxTimeoutSeconds: 60},
	}
	p := newTestPlugin(routes, &mockFacilitator{})

	// POST matches.
	rc := p.matchRoute("POST", "/api/data")
	assert.NotNil(t, rc)

	// GET does not match.
	rc = p.matchRoute("GET", "/api/data")
	assert.Nil(t, rc)
}

func TestInitRequiresFacilitator(t *testing.T) {
	p := New()
	err := p.Init(nil)
	assert.Error(t, err)

	err = p.Init(map[string]interface{}{})
	assert.Error(t, err)
}

func TestInitParsesRoutes(t *testing.T) {
	p := New()
	err := p.Init(map[string]interface{}{
		"facilitator": "https://x402.org/facilitator",
		"pay_to":      "0xGLOBAL",
		"network":     "eip155:1",
		"routes": []map[string]interface{}{
			{
				"path":  "/api/premium/*",
				"price": "0.05",
			},
			{
				"path":     "GET /api/weather",
				"price":    "0.001",
				"currency": "ETH",
				"pay_to":   "0xOVERRIDE",
				"network":  "eip155:8453",
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, p.routes, 2)

	// First route inherits global pay_to and network.
	assert.Equal(t, "0xGLOBAL", p.routes[0].payTo)
	assert.Equal(t, "eip155:1", p.routes[0].network)
	assert.Equal(t, "USDC", p.routes[0].asset)

	// Second route overrides.
	assert.Equal(t, "0xOVERRIDE", p.routes[1].payTo)
	assert.Equal(t, "eip155:8453", p.routes[1].network)
	assert.Equal(t, "ETH", p.routes[1].asset)
}

func TestPaymentRequiredHeaderDecodable(t *testing.T) {
	// Verify the Payment-Required header can be decoded back.
	pr := &PaymentRequired{
		X402Version: 1,
		Resource:    ResourceInfo{URL: "http://example.com/api/data"},
		Accepts: []PaymentRequirements{
			{Scheme: "exact", Network: "eip155:8453", Asset: "USDC", Amount: "0.01", PayTo: "0x123", MaxTimeoutSeconds: 60, Extra: map[string]interface{}{}},
		},
	}
	encoded := encodePaymentRequired(pr)

	data, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)

	var decoded PaymentRequired
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, pr.X402Version, decoded.X402Version)
	assert.Equal(t, pr.Accepts[0].Amount, decoded.Accepts[0].Amount)
}
