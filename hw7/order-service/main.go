package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sns"
)

type Item struct {
	ProductID string  `json:"product_id"`
	Quantity  int     `json:"quantity"`
	Price     float64 `json:"price"`
}

type Order struct {
	OrderID    string    `json:"order_id"`
	CustomerID int       `json:"customer_id"`
	Status     string    `json:"status"`
	Items      []Item    `json:"items"`
	CreatedAt  time.Time `json:"created_at"`
}

var (
	snsClient   *sns.Client
	snsTopicARN string
	paymentMu   sync.Mutex
)

func main() {
	snsTopicARN = os.Getenv("SNS_TOPIC_ARN")
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}

	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(region),
	)
	if err != nil {
		log.Fatalf("Failed to load AWS config: %v", err)
	}
	snsClient = sns.NewFromConfig(cfg)

	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/orders/sync", syncOrderHandler)
	http.HandleFunc("/orders/async", asyncOrderHandler)

	log.Println("Order receiver listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func syncOrderHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var order Order
	if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	order.OrderID = fmt.Sprintf("ord-%d", time.Now().UnixNano())
	order.CreatedAt = time.Now()
	order.Status = "processing"

	// Simulate payment bottleneck — only one payment at a time, 3 seconds each
	paymentMu.Lock()
	log.Printf("Processing payment for order %s", order.OrderID)
	time.Sleep(3 * time.Second)
	log.Printf("Payment complete for order %s", order.OrderID)
	paymentMu.Unlock()

	order.Status = "completed"
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(order)
}

func asyncOrderHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var order Order
	if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	order.OrderID = fmt.Sprintf("ord-%d", time.Now().UnixNano())
	order.CreatedAt = time.Now()
	order.Status = "pending"

	orderJSON, _ := json.Marshal(order)
	_, err := snsClient.Publish(context.Background(), &sns.PublishInput{
		TopicArn: aws.String(snsTopicARN),
		Message:  aws.String(string(orderJSON)),
	})
	if err != nil {
		log.Printf("Failed to publish to SNS: %v", err)
		http.Error(w, "failed to queue order", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"order_id": order.OrderID,
		"status":   "accepted",
		"message":  "Order queued for processing",
	})
}
