package main

import (
	"os"

	"github.com/rykhalskyi/killswitch/internal/cli"
)

func main() {
	code := cli.Main(os.Args)
	os.Exit(code)
}
