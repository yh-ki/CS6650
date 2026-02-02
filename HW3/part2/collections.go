package main

import (
	"fmt"
	"sync"
)

func main() {
	m := make(map[int]int)

	var wg sync.WaitGroup

	for g := 0; g < 50; g++ {
		wg.Go(func() {
			for i := 0; i < 1000; i++ {
				m[g*1000+i] = i
			}
		})
	}

	wg.Wait()

	fmt.Println("Length:", len(m))
}
