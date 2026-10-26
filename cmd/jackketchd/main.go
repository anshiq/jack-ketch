package main

import (
	"fmt"
	"os"

	"jack-ketch/internal/jackketchd"
)

var version = "v0.2.0"

func main() {
	if err := jackketchd.Main(os.Args[1:], version); err != nil {
		fmt.Fprintln(os.Stderr, "jackketchd:", err)
		os.Exit(1)
	}
}
