package main

import (
	"bufio"
	"fmt"
	"os"
	"time"
)

const iterations = 100_000

func WriteUnbuffered(path string) time.Duration {
	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}

	start := time.Now()
	for i := 0; i < iterations; i++ {
		f.Write([]byte(fmt.Sprintf("line %d\n", i)))
	}
	elapsed := time.Since(start)

	f.Close()
	return elapsed
}

func WriteBuffered(path string) time.Duration {
	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}

	w := bufio.NewWriter(f) // default buffer size = 4096 bytes

	start := time.Now()
	for i := 0; i < iterations; i++ {
		w.WriteString(fmt.Sprintf("line %d\n", i))
	}
	w.Flush()
	elapsed := time.Since(start)

	f.Close()
	return elapsed
}

func main() {
	unbuffered := WriteUnbuffered("./out_unbuffered.txt")
	buffered := WriteBuffered("./out_buffered.txt")

	fmt.Printf("Unbuffered : %v\n", unbuffered)
	fmt.Printf("Buffered   : %v\n", buffered)
	fmt.Printf("Speedup    : %.2fx\n", float64(unbuffered)/float64(buffered))
}
