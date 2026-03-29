package demo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestDemoServer(t *testing.T) {
	srv := NewServer()
	port, err := srv.Start()
	if err != nil {
		t.Fatalf("failed to start demo server: %v", err)
	}
	defer srv.Close()

	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	t.Run("health", func(t *testing.T) {
		resp, err := http.Get(base + "/health")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var body map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&body)
		if body["status"] != "ok" {
			t.Fatalf("expected status ok, got %v", body["status"])
		}
	})

	t.Run("list products", func(t *testing.T) {
		resp, err := http.Get(base + "/products")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var body map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&body)
		total := int(body["total"].(float64))
		if total != 5 {
			t.Fatalf("expected 5 products, got %d", total)
		}
	})

	t.Run("get product", func(t *testing.T) {
		resp, err := http.Get(base + "/products/prod-1")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		var body Product
		json.NewDecoder(resp.Body).Decode(&body)
		if body.ID != "prod-1" {
			t.Fatalf("expected prod-1, got %s", body.ID)
		}
	})

	t.Run("product not found returns HTML (not agent-friendly)", func(t *testing.T) {
		resp, err := http.Get(base + "/products/nonexistent")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "text/html") {
			t.Fatalf("expected text/html, got %s", ct)
		}
	})

	t.Run("filter by category", func(t *testing.T) {
		resp, err := http.Get(base + "/products?category=home")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&body)
		total := int(body["total"].(float64))
		if total != 1 {
			t.Fatalf("expected 1 home product, got %d", total)
		}
	})

	t.Run("create product", func(t *testing.T) {
		resp, err := http.Post(base+"/products", "application/json",
			strings.NewReader(`{"name":"Test Product","price":9.99,"category":"test"}`))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 201 {
			t.Fatalf("expected 201, got %d", resp.StatusCode)
		}
	})

	t.Run("list users", func(t *testing.T) {
		resp, err := http.Get(base + "/users")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&body)
		total := int(body["total"].(float64))
		if total != 3 {
			t.Fatalf("expected 3 users, got %d", total)
		}
	})

	t.Run("URL method", func(t *testing.T) {
		url := srv.URL()
		if url != fmt.Sprintf("http://127.0.0.1:%d", port) {
			t.Fatalf("unexpected URL: %s", url)
		}
	})
}
