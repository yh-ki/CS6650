package main

import (
	"fmt"
	"runtime"
	"time"
)

func swi() {
	ch := make(chan struct{}) // unbuffered channel of empty signals
	done := make(chan struct{})

	const rounds = 1_000_000

	// Goroutine A: sends first, then receives (rounds - 1) more times
	go func() {
		for i := 0; i < rounds; i++ {
			ch <- struct{}{} // send
			if i < rounds-1 {
				<-ch // receive (skip the very last receive so we don't deadlock)
			}
		}
	}()

	// Goroutine B: receives first, then sends back — repeat
	go func() {
		for i := 0; i < rounds; i++ {
			<-ch // receive
			if i < rounds-1 {
				ch <- struct{}{} // send back
			}
		}
		done <- struct{}{} // signal completion
	}()

	start := time.Now()
	<-done
	elapsed := time.Since(start)

	// Each round-trip contains exactly 2 channel hand-offs (A→B, then B→A),
	// so total hand-offs = 2 × rounds.  Dividing gives per-switch cost.
	totalSwitches := 2 * rounds
	avgSwitch := elapsed / time.Duration(totalSwitches)

	fmt.Print("Total duration :", elapsed)
	fmt.Print(", Avg switch time:", avgSwitch)
}

func main() {
	fmt.Print("Single OS thread mode:")
	runtime.GOMAXPROCS(1)
	swi()

	fmt.Printf("\n")

	fmt.Print("Multi OS thread mode:")
	runtime.SetDefaultGOMAXPROCS()
	swi()
}
