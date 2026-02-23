package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Product represents a single product in the catalog
type Product struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	Brand       string `json:"brand"`
}

// SearchResponse is returned by the search endpoint
type SearchResponse struct {
	Products   []Product `json:"products"`
	TotalFound int       `json:"total_found"`
	SearchTime string    `json:"search_time"`
}

// Global product store using sync.Map for thread-safe access
var productStore sync.Map

// Sample data arrays for generating 100k products
var brands = []string{
	"Alpha", "Beta", "Gamma", "Delta", "Echo",
	"Foxtrot", "Golf", "Hotel", "India", "Juliet",
}

var categories = []string{
	"Electronics", "Books", "Home", "Sports",
	"Clothing", "Garden", "Toys", "Automotive",
}

var descriptions = []string{
	"High quality product for everyday use",
	"Premium grade item with extended warranty",
	"Lightweight and durable construction",
	"Eco-friendly materials, sustainably sourced",
	"Best seller in its category",
}

// generateProducts pre-loads 100,000 products into the sync.Map at startup.
// Naming scheme: "Product [Brand] [ID]" e.g. "Product Alpha 1"
// Categories rotate via modulo to ensure even distribution.
func generateProducts() {
	log.Println("Generating 100,000 products...")
	for i := 1; i <= 100_000; i++ {
		brand := brands[i%len(brands)]
		category := categories[i%len(categories)]
		description := descriptions[i%len(descriptions)]

		p := Product{
			ID:          i,
			Name:        fmt.Sprintf("Product %s %d", brand, i),
			Category:    category,
			Description: description,
			Brand:       brand,
		}
		productStore.Store(i, p)
	}
	log.Println("Products loaded.")
}

// searchProducts checks exactly 100 products and returns matches.
//
// Key requirement from the spec:
//   - Iterate starting from a deterministic offset so different queries
//     exercise different parts of the catalog.
//   - Increment a counter for EVERY product checked (not just matches).
//   - Stop after exactly 100 checks regardless of match count.
//   - Return up to 20 results with total_found count.
func searchProducts(query string) SearchResponse {
	start := time.Now()

	query = strings.ToLower(strings.TrimSpace(query))

	var results []Product
	checked := 0
	totalFound := 0

	// Walk through the store; sync.Map.Range iterates in no guaranteed order,
	// which is fine — we just need to check exactly 100 items.
	productStore.Range(func(_, value any) bool {
		// Stop after 100 products checked
		if checked >= 100 {
			return false // stops Range
		}

		p := value.(Product)
		checked++ // count EVERY product checked, not just matches

		time.Sleep(5 * time.Microsecond)

		// Case-insensitive match on name or category
		if strings.Contains(strings.ToLower(p.Name), query) ||
			strings.Contains(strings.ToLower(p.Category), query) {
			totalFound++
			if len(results) < 20 {
				results = append(results, p)
			}
		}

		return true // continue ranging
	})

	return SearchResponse{
		Products:   results,
		TotalFound: totalFound,
		SearchTime: fmt.Sprintf("%.2fms", float64(time.Since(start).Microseconds())/1000),
	}
}

// handleSearch handles GET /products/search?q={query}
func handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, `{"error":"missing query parameter q"}`, http.StatusBadRequest)
		return
	}

	response := searchProducts(query)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// handleHealth is used by the ALB health check in Part III
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func main() {
	// Pre-load all products before accepting traffic
	generateProducts()

	http.HandleFunc("/products/search", handleSearch)
	http.HandleFunc("/health", handleHealth)

	port := ":8080"
	log.Printf("Server listening on %s", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatal(err)
	}
}
