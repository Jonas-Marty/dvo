package main

import (
	"os"

	"github.com/Jonas-Marty/ad-cli/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
