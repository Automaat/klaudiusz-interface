package main

import (
	"fmt"
	"os"
)

func main() {
	for _, arg := range os.Args[1:] {
		if arg == "--resume" {
			fmt.Println("Resumed successfully")
			os.Exit(0)
		}
	}
	fmt.Fprintln(os.Stderr, "Expected --resume flag")
	os.Exit(1)
}
