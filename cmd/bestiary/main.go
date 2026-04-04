package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "bestiary: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	return nil // stub — implemented in L3
}
