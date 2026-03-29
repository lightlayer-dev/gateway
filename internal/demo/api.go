// Package demo provides a built-in sample API for demo mode.
// It serves a simple JSON API that the gateway can proxy to demonstrate
// agent-readiness features without requiring the user to have their own API.
package demo

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
)

// Product represents a sample product.
type Product struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Price       float64 `json:"price"`
	Category    string  `json:"category"`
	Description string  `json:"description"`
	InStock     bool    `json:"in_stock"`
}

// User represents a sample user.
type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

var seedProducts = []Product{
	{ID: "prod-1", Name: "Wireless Earbuds Pro", Price: 79.99, Category: "electronics", Description: "Premium noise-cancelling earbuds with 30h battery", InStock: true},
	{ID: "prod-2", Name: "USB-C Hub 7-in-1", Price: 49.99, Category: "electronics", Description: "Aluminum hub with HDMI, USB-A, SD card reader", InStock: true},
	{ID: "prod-3", Name: "Mechanical Keyboard", Price: 129.99, Category: "electronics", Description: "Cherry MX Brown switches, RGB backlit", InStock: true},
	{ID: "prod-4", Name: "Pour-Over Coffee Set", Price: 39.99, Category: "home", Description: "Ceramic dripper, glass carafe, 40 filters", InStock: true},
	{ID: "prod-5", Name: "Canvas Sneakers", Price: 44.99, Category: "clothing", Description: "Classic low-top, cushioned insole", InStock: false},
}

var seedUsers = []User{
	{ID: "user-1", Name: "Alice Chen", Email: "alice@example.com", Role: "admin"},
	{ID: "user-2", Name: "Bob Smith", Email: "bob@example.com", Role: "developer"},
	{ID: "user-3", Name: "Carol Davis", Email: "carol@example.com", Role: "viewer"},
}

// Server is the demo API server.
type Server struct {
	httpSrv  *http.Server
	port     int
	products []Product
	mu       sync.RWMutex
}

// NewServer creates a new demo API server.
func NewServer() *Server {
	s := &Server{
		products: append([]Product{}, seedProducts...),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /products", s.handleListProducts)
	mux.HandleFunc("GET /products/{id}", s.handleGetProduct)
	mux.HandleFunc("POST /products", s.handleCreateProduct)
	mux.HandleFunc("GET /users", s.handleListUsers)
	mux.HandleFunc("GET /users/{id}", s.handleGetUser)

	// Fallback — no agent-friendly errors (that's what the gateway adds!)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(404)
		fmt.Fprintf(w, "<html><body><h1>404 Not Found</h1><p>%s %s</p></body></html>", r.Method, r.URL.Path)
	})

	s.httpSrv = &http.Server{Handler: mux}
	return s
}

// Start listens on a random available port and returns it.
func (s *Server) Start() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("demo API listen: %w", err)
	}
	s.port = ln.Addr().(*net.TCPAddr).Port

	go s.httpSrv.Serve(ln)
	return s.port, nil
}

// Port returns the port the demo server is listening on.
func (s *Server) Port() int { return s.port }

// Close shuts down the demo server.
func (s *Server) Close() error {
	if s.httpSrv != nil {
		return s.httpSrv.Close()
	}
	return nil
}

// URL returns the base URL of the demo server.
func (s *Server) URL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", s.port)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]interface{}{
		"status":  "ok",
		"service": "Demo API",
		"version": "1.0.0",
	})
}

func (s *Server) handleListProducts(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	category := r.URL.Query().Get("category")
	if category != "" {
		filtered := make([]Product, 0)
		for _, p := range s.products {
			if strings.EqualFold(p.Category, category) {
				filtered = append(filtered, p)
			}
		}
		writeJSON(w, 200, map[string]interface{}{"data": filtered, "total": len(filtered)})
		return
	}

	writeJSON(w, 200, map[string]interface{}{"data": s.products, "total": len(s.products)})
}

func (s *Server) handleGetProduct(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, p := range s.products {
		if p.ID == id {
			writeJSON(w, 200, p)
			return
		}
	}
	// Deliberately NOT agent-friendly — HTML error. The gateway fixes this.
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(404)
	fmt.Fprintf(w, "<html><body><h1>Product not found</h1></body></html>")
}

func (s *Server) handleCreateProduct(w http.ResponseWriter, r *http.Request) {
	var p Product
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.WriteHeader(400)
		fmt.Fprintf(w, "Bad request: %v", err)
		return
	}
	s.mu.Lock()
	p.ID = fmt.Sprintf("prod-%d", len(s.products)+1)
	s.products = append(s.products, p)
	s.mu.Unlock()

	writeJSON(w, 201, p)
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]interface{}{"data": seedUsers, "total": len(seedUsers)})
}

func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	for _, u := range seedUsers {
		if u.ID == id {
			writeJSON(w, 200, u)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(404)
	fmt.Fprintf(w, "<html><body><h1>User not found</h1></body></html>")
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
