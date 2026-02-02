package main

import (
	"fmt"
	"sync"
	"time"
)

func main() {

	var m sync.Map

	var wg sync.WaitGroup

	start := time.Now()

	for g := 0; g < 50; g++ {
		wg.Go(func() {
			for i := 0; i < 1000; i++ {
				m.Store(g*1000, i)
			}
		})
	}

	wg.Wait()

	end := time.Now()

	t := end.Sub(start)

	count := 0
	m.Range(func(key, value interface{}) bool {
		count++
		return true
	})

	fmt.Println("Length:", count, ", Time", t)
}
