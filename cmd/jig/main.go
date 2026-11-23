// Command jig runs declarative Kubernetes tests (see package cli).
package main

import (
	"github.com/intentd/jig/cmd/cli"
	"github.com/intentd/tools/term"
)

func main() {
	if err := cli.Command().Execute(); err != nil {
		term.FatalExit("jig", err)
	}
}
