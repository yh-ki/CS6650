package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

func main() {

	var ops atomic.Uint64

	var wg sync.WaitGroup

	for range 50 {
		wg.Go(func() {
			for range 1000 {

				ops.Add(1)
			}
		})
	}

	wg.Wait()

	fmt.Println("ops:", ops.Load())

	var non_atom uint64

	for range 50 {
		wg.Go(func() {
			for range 1000 {
				non_atom++
			}
		})
	}

	wg.Wait()

	fmt.Println("Non atomic:", non_atom)

}
