package main

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
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

func handler(ctx context.Context, snsEvent events.SNSEvent) error {
	for _, record := range snsEvent.Records {
		snsMessage := record.SNS.Message

		var order Order
		if err := json.Unmarshal([]byte(snsMessage), &order); err != nil {
			log.Printf("Failed to parse order: %v", err)
			continue
		}

		log.Printf("Processing order %s for customer %d", order.OrderID, order.CustomerID)

		// Simulate 3-second payment processing
		time.Sleep(3 * time.Second)

		log.Printf("Completed order %s", order.OrderID)
	}
	return nil
}

func main() {
	lambda.Start(handler)
}
