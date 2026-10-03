// Command cclayer syncs Claude Code configuration across machines in layers:
// one public base plus one private overlay per organization.
package main

import (
	"os"

	"github.com/zhaojiannet/cclayer/internal/cli"
)

func main() {
	env, err := cli.DefaultEnv()
	if err != nil {
		os.Stderr.WriteString("cclayer: " + err.Error() + "\n")
		os.Exit(1)
	}
	os.Exit(cli.Run(env, os.Args[1:]))
}
