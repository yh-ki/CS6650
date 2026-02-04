package main

import (
	"fmt"
	"sync"
	"time"
)

type Container struct {
	mu sync.RWMutex
	m  map[int]int
}

func (c *Container) work(rou int, val int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[rou] = val
}

func main() {

	c := Container{
		m: map[int]int{},
	}

	var wg sync.WaitGroup

	start := time.Now()

	for g := 0; g < 50; g++ {
		wg.Go(func() {
			for i := 0; i < 1000; i++ {
				c.work(g*1000+i, i)
			}
		})
	}

	wg.Wait()

	end := time.Now()

	t := end.Sub(start)

	fmt.Println("Length:", len(c.m), ", Time", t)
}
