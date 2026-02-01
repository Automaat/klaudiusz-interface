package main

import (
	"fmt"
	"time"
)

func main() {
	// Artificial delay to test serialization
	time.Sleep(100 * time.Millisecond)
	fmt.Println("Success")
}
