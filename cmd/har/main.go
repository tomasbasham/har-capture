package main

import (
	"os"

	cliruntime "github.com/tomasbasham/cli-runtime"
	"github.com/tomasbasham/har-capture/internal/cmd"
)

func main() {
	command := cmd.NewRootCommand()
	if code := cliruntime.Run(command); code != 0 {
		os.Exit(code)
	}
}
