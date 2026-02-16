package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"

	"github.com/gorilla/mux"
)

// Product represents the product model as defined in the OpenAPI spec
type Product struct {
	ProductID    int32  `json:"product_id"`
	SKU          string `json:"sku"`
	Manufacturer string `json:"manufacturer"`
	CategoryID   int32  `json:"category_id"`
	Weight       int32  `json:"weight"`
	SomeOtherID  int32  `json:"some_other_id"`
}

// Error represents the error response model
type Error struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// ProductStore manages products in memory with thread safety
type ProductStore struct {
	mu       sync.RWMutex
	products map[int32]Product
	nextID   int32
}

// NewProductStore creates a new product store
func NewProductStore() *ProductStore {
	return &ProductStore{
		products: make(map[int32]Product),
		nextID:   1,
	}
}

// GetProduct handles GET /products/{productId}
func (ps *ProductStore) GetProduct(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	productIDStr := vars["productId"]

	// Parse and validate productId
	productID64, err := strconv.ParseInt(productIDStr, 10, 32)
	if err != nil || productID64 < 1 {
		respondWithError(w, http.StatusBadRequest, "INVALID_INPUT", 
			"Invalid product ID", "Product ID must be a positive integer")
		return
	}
	productID := int32(productID64)

	// Retrieve product
	ps.mu.RLock()
	product, exists := ps.products[productID]
	ps.mu.RUnlock()

	if !exists {
		respondWithError(w, http.StatusNotFound, "NOT_FOUND", 
			"Product not found", "No product exists with the given ID")
		return
	}

	respondWithJSON(w, http.StatusOK, product)
}

// AddProductDetails handles POST /products/{productId}/details
func (ps *ProductStore) AddProductDetails(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	productIDStr := vars["productId"]

	// Parse and validate productId
	productID64, err := strconv.ParseInt(productIDStr, 10, 32)
	if err != nil || productID64 < 1 {
		respondWithError(w, http.StatusBadRequest, "INVALID_INPUT", 
			"Invalid product ID", "Product ID must be a positive integer")
		return
	}
	productID := int32(productID64)

	// Parse request body
	var product Product
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&product); err != nil {
		respondWithError(w, http.StatusBadRequest, "INVALID_INPUT", 
			"Invalid request body", "Unable to parse JSON request body")
		return
	}
	defer r.Body.Close()

	// Validate required fields
	if err := validateProduct(product); err != nil {
		respondWithError(w, http.StatusBadRequest, "INVALID_INPUT", 
			"Invalid product data", err.Error())
		return
	}

	// Set the product ID from path parameter
	product.ProductID = productID

	// Check if product exists (for update) or create new
	ps.mu.Lock()
	ps.products[productID] = product
	ps.mu.Unlock()

	// Return 204 No Content
	w.WriteHeader(http.StatusNoContent)
}

// validateProduct validates all required fields of a product
func validateProduct(p Product) error {
	if p.SKU == "" {
		return &ValidationError{"SKU is required"}
	}
	if len(p.SKU) > 100 {
		return &ValidationError{"SKU must be at most 100 characters"}
	}
	if p.Manufacturer == "" {
		return &ValidationError{"Manufacturer is required"}
	}
	if len(p.Manufacturer) > 200 {
		return &ValidationError{"Manufacturer must be at most 200 characters"}
	}
	if p.CategoryID < 1 {
		return &ValidationError{"Category ID must be a positive integer"}
	}
	if p.Weight < 0 {
		return &ValidationError{"Weight must be non-negative"}
	}
	if p.SomeOtherID < 1 {
		return &ValidationError{"Some other ID must be a positive integer"}
	}
	return nil
}

// ValidationError represents a validation error
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// respondWithError sends an error response
func respondWithError(w http.ResponseWriter, code int, errorCode, message, details string) {
	errorResponse := Error{
		Error:   errorCode,
		Message: message,
		Details: details,
	}
	respondWithJSON(w, code, errorResponse)
}

// respondWithJSON sends a JSON response
func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, err := json.Marshal(payload)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}

// loggingMiddleware logs all HTTP requests
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s %s", r.Method, r.RequestURI, r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}

// healthCheck handles health check endpoint
func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func main() {
	// Initialize product store
	store := NewProductStore()

	// Setup router
	router := mux.NewRouter()
	router.Use(loggingMiddleware)

	// Health check endpoint
	router.HandleFunc("/health", healthCheck).Methods("GET")

	// Product endpoints (only implementing Product API for this assignment)
	router.HandleFunc("/products/{productId}", store.GetProduct).Methods("GET")
	router.HandleFunc("/products/{productId}/details", store.AddProductDetails).Methods("POST")

	// Start server
	port := ":8080"
	log.Printf("Server starting on port %s", port)
	if err := http.ListenAndServe(port, router); err != nil {
		log.Fatal(err)
	}
}
