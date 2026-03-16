package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
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

type SNSEnvelope struct {
	Message string `json:"Message"`
}

var (
	sqsClient   *sqs.Client
	sqsQueueURL string
	numWorkers  int
)

func main() {
	sqsQueueURL = os.Getenv("SQS_QUEUE_URL")
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}

	numWorkers = 1
	if w := os.Getenv("NUM_WORKERS"); w != "" {
		if n, err := strconv.Atoi(w); err == nil {
			numWorkers = n
		}
	}

	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(region),
	)
	if err != nil {
		log.Fatalf("Failed to load AWS config: %v", err)
	}
	sqsClient = sqs.NewFromConfig(cfg)

	log.Printf("Starting order processor with %d workers", numWorkers)

	// Health check endpoint
	go func() {
		http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
		})
		http.ListenAndServe(":8081", nil)
	}()

	// Semaphore channel — this is the key fix.
	// Limits concurrent payment processing to exactly numWorkers.
	sem := make(chan struct{}, numWorkers)

	// Single polling loop — pulls batches and dispatches to worker pool
	var wg sync.WaitGroup
	for {
		result, err := sqsClient.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(sqsQueueURL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     20,
		})
		if err != nil {
			log.Printf("Error receiving messages: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		for _, msg := range result.Messages {
			// Acquire semaphore slot — blocks if numWorkers slots are taken
			sem <- struct{}{}
			wg.Add(1)
			go func(m types.Message) {
				defer wg.Done()
				defer func() { <-sem }() // release slot when done
				processMessage(m)
			}(msg)
		}
	}
}

func processMessage(m types.Message) {
	var envelope SNSEnvelope
	if err := json.Unmarshal([]byte(*m.Body), &envelope); err != nil {
		log.Printf("Failed to parse SNS envelope: %v", err)
		return
	}

	var order Order
	if err := json.Unmarshal([]byte(envelope.Message), &order); err != nil {
		log.Printf("Failed to parse order: %v", err)
		return
	}

	log.Printf("Processing order %s", order.OrderID)
	time.Sleep(3 * time.Second) // simulate payment processing
	log.Printf("Completed order %s", order.OrderID)

	sqsClient.DeleteMessage(context.Background(), &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(sqsQueueURL),
		ReceiptHandle: m.ReceiptHandle,
	})
}
