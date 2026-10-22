package main

import (
	"os"

	"lenovo-driver/internal/gui"
)

func main() {
	os.Exit(gui.Run(os.Args[1:]))
}
