package main

import (
	"os"

	"lenovo-driver/internal/app"
)

func main() {
	os.Exit(app.New(os.Stdout, os.Stderr, os.Stdin).Run(os.Args[1:]))
}
