package main

import (
	"fmt"
	"os"
)

func main() {
	pwd, err := os.Getwd()
	if err != nil {
		pwd = "ERROR"
	}
	// Output PERMISSION_REQUIRED format with actual PWD
	// to test working directory functionality
	fmt.Printf("PERMISSION_REQUIRED: Test action description | COMMANDS: pwd=%s", pwd)
}
