package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

type SplitRequest struct {
	S3URL      string `json:"s3_url"`
	NumChunks  int    `json:"num_chunks"`
	BucketName string `json:"bucket_name"`
}

type SplitResponse struct {
	ChunkURLs []string `json:"chunk_urls"`
	Status    string   `json:"status"`
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

func splitHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SplitRequest

	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
	} else {
		req.S3URL = r.URL.Query().Get("s3_url")
		req.BucketName = r.URL.Query().Get("bucket_name")
		numChunks := r.URL.Query().Get("num_chunks")
		if numChunks == "" {
			numChunks = "3"
		}
		fmt.Sscanf(numChunks, "%d", &req.NumChunks)
	}

	if req.NumChunks == 0 {
		req.NumChunks = 3
	}

	log.Printf("Splitting file: %s into %d chunks", req.S3URL, req.NumChunks)

	// Download file from S3
	content, err := downloadFromS3(req.S3URL, req.BucketName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to download file: %v", err), http.StatusInternalServerError)
		return
	}

	// Split content into chunks
	chunks := splitContent(content, req.NumChunks)

	// Upload chunks to S3
	chunkURLs := make([]string, len(chunks))
	for i, chunk := range chunks {
		key := fmt.Sprintf("chunks/chunk_%d.txt", i)
		url, err := uploadToS3(req.BucketName, key, chunk)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to upload chunk %d: %v", i, err), http.StatusInternalServerError)
			return
		}
		chunkURLs[i] = url
	}

	response := SplitResponse{
		ChunkURLs: chunkURLs,
		Status:    "success",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func downloadFromS3(s3URL, bucketName string) (string, error) {
	// Extract key from S3 URL or use as key directly
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

func splitContent(content string, numChunks int) []string {
	words := strings.Fields(content)
	totalWords := len(words)
	chunkSize := (totalWords + numChunks - 1) / numChunks

	chunks := make([]string, 0, numChunks)
	for i := 0; i < numChunks; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > totalWords {
			end = totalWords
		}
		if start < totalWords {
			chunks = append(chunks, strings.Join(words[start:end], " "))
		}
	}

	return chunks
}

func uploadToS3(bucketName, key, content string) (string, error) {
	_, err := s3Uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
		Body:   bytes.NewReader([]byte(content)),
	})
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("s3://%s/%s", bucketName, key), nil
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Splitter service is healthy")
}

func main() {
	http.HandleFunc("/split", splitHandler)
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/", healthHandler)

	port := getEnv("PORT", "8080")
	log.Printf("Splitter service starting on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
