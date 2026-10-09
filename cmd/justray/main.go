package main

import (
	"os"

	"github.com/luynrs/justray/internal/client/cli"
)

func main() {
	os.Exit(cli.Execute())
}
