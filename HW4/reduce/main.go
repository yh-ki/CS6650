package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

type ReduceRequest struct {
	ResultURLs []string `json:"result_urls"`
	BucketName string   `json:"bucket_name"`
}

type ReduceResponse struct {
	FinalURL    string      `json:"final_url"`
	TotalWords  int         `json:"total_words"`
	UniqueWords int         `json:"unique_words"`
	TopWords    []WordCount `json:"top_words"`
	Status      string      `json:"status"`
}

type WordCount struct {
	Word  string `json:"word"`
	Count int    `json:"count"`
}

var (
	sess         *session.Session
	s3Client     *s3.S3
	s3Uploader   *s3manager.Uploader
	s3Downloader *s3manager.Downloader
)

func init() {
	var err error
	sess, err = session.NewSession(&aws.Config{
		Region: aws.String(getEnv("AWS_REGION", "us-west-2")),
	})
	if err != nil {
		log.Fatal("Failed to create AWS session:", err)
	}

	s3Client = s3.New(sess)
	s3Uploader = s3manager.NewUploader(sess)
	s3Downloader = s3manager.NewDownloader(sess)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func reduceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ReduceRequest

	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
	} else {
		urlsParam := r.URL.Query().Get("result_urls")
		if urlsParam != "" {
			req.ResultURLs = strings.Split(urlsParam, ",")
		}
		req.BucketName = r.URL.Query().Get("bucket_name")
	}

	log.Printf("Reducing %d results", len(req.ResultURLs))

	// Download and aggregate all results
	aggregatedCounts := make(map[string]int)

	for i, url := range req.ResultURLs {
		log.Printf("Downloading result %d: %s", i+1, url)
		counts, err := downloadResults(url, req.BucketName)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to download result %d: %v", i, err), http.StatusInternalServerError)
			return
		}

		// Aggregate counts
		for word, count := range counts {
			aggregatedCounts[word] += count
		}
	}

	// Calculate statistics
	totalWords := 0
	for _, count := range aggregatedCounts {
		totalWords += count
	}

	// Get top 10 words
	topWords := getTopWords(aggregatedCounts, 10)

	// Upload final results to S3
	finalKey := "results/final_word_counts.json"
	finalURL, err := uploadResultsToS3(req.BucketName, finalKey, aggregatedCounts)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to upload final results: %v", err), http.StatusInternalServerError)
		return
	}

	response := ReduceResponse{
		FinalURL:    finalURL,
		TotalWords:  totalWords,
		UniqueWords: len(aggregatedCounts),
		TopWords:    topWords,
		Status:      "success",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func downloadResults(s3URL, bucketName string) (map[string]int, error) {
	key := strings.TrimPrefix(s3URL, "s3://"+bucketName+"/")
	key = strings.TrimPrefix(key, "/")

	buf := aws.NewWriteAtBuffer([]byte{})
	_, err := s3Downloader.Download(buf, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}

	var counts map[string]int
	if err := json.Unmarshal(buf.Bytes(), &counts); err != nil {
		return nil, err
	}

	return counts, nil
}

func getTopWords(wordCounts map[string]int, n int) []WordCount {
	// Convert map to slice
	words := make([]WordCount, 0, len(wordCounts))
	for word, count := range wordCounts {
		words = append(words, WordCount{Word: word, Count: count})
	}

	// Sort by count (descending)
	sort.Slice(words, func(i, j int) bool {
		return words[i].Count > words[j].Count
	})

	// Return top N
	if len(words) < n {
		n = len(words)
	}
	return words[:n]
}

func uploadResultsToS3(bucketName, key string, wordCounts map[string]int) (string, error) {
	jsonData, err := json.Marshal(wordCounts)
	if err != nil {
		return "", err
	}

	_, err = s3Uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
		Body:   bytes.NewReader(jsonData),
	})
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("s3://%s/%s", bucketName, key), nil
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Reducer service is healthy")
}

func main() {
	http.HandleFunc("/reduce", reduceHandler)
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/", healthHandler)

	port := getEnv("PORT", "8080")
	log.Printf("Reducer service starting on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
