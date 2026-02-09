package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

type MapRequest struct {
	ChunkURL   string `json:"chunk_url"`
	BucketName string `json:"bucket_name"`
	MapperID   string `json:"mapper_id"`
}

type MapResponse struct {
	ResultURL string `json:"result_url"`
	WordCount int    `json:"word_count"`
	Status    string `json:"status"`
}

var (
	sess         *session.Session
	s3Client     *s3.S3
	s3Uploader   *s3manager.Uploader
	s3Downloader *s3manager.Downloader
	wordRegex    *regexp.Regexp
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
	wordRegex = regexp.MustCompile(`[a-zA-Z]+`)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func mapHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req MapRequest

	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
	} else {
		req.ChunkURL = r.URL.Query().Get("chunk_url")
		req.BucketName = r.URL.Query().Get("bucket_name")
		req.MapperID = r.URL.Query().Get("mapper_id")
	}

	if req.MapperID == "" {
		req.MapperID = "mapper_0"
	}

	log.Printf("Mapping chunk: %s (mapper: %s)", req.ChunkURL, req.MapperID)

	// Download chunk from S3
	content, err := downloadFromS3(req.ChunkURL, req.BucketName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to download chunk: %v", err), http.StatusInternalServerError)
		return
	}

	// Count words
	wordCounts := countWords(content)

	// Upload results to S3
	resultKey := fmt.Sprintf("results/%s_result.json", req.MapperID)
	resultURL, err := uploadResultsToS3(req.BucketName, resultKey, wordCounts)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to upload results: %v", err), http.StatusInternalServerError)
		return
	}

	totalWords := 0
	for _, count := range wordCounts {
		totalWords += count
	}

	response := MapResponse{
		ResultURL: resultURL,
		WordCount: totalWords,
		Status:    "success",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func downloadFromS3(s3URL, bucketName string) (string, error) {
	key := strings.TrimPrefix(s3URL, "s3://"+bucketName+"/")
	key = strings.TrimPrefix(key, "/")

	buf := aws.NewWriteAtBuffer([]byte{})
	_, err := s3Downloader.Download(buf, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
	})
	if err != nil {
		return "", err
	}

	return string(buf.Bytes()), nil
}

func countWords(content string) map[string]int {
	wordCounts := make(map[string]int)

	// Extract all words and convert to lowercase
	words := wordRegex.FindAllString(content, -1)

	for _, word := range words {
		word = strings.ToLower(word)
		wordCounts[word]++
	}

	return wordCounts
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
	fmt.Fprintf(w, "Mapper service is healthy")
}

func main() {
	http.HandleFunc("/map", mapHandler)
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/", healthHandler)

	port := getEnv("PORT", "8080")
	log.Printf("Mapper service starting on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
