// The clickup-axi binary. main is the composition root: it wires the
// real ClickUp client and updater (including this binary's rendered
// skill for self-healing installed copies) and hands off to the CLI.
package main

import (
	"os"
	_ "time/tzdata"

	"github.com/JanSuthacheeva/clickup-axi/internal/cli"
	"github.com/JanSuthacheeva/clickup-axi/internal/clickup"
	"github.com/JanSuthacheeva/clickup-axi/internal/update"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], clickup.NewFromEnv(), update.NewFromEnv(cli.GenerateSkill()), os.Stdin, os.Stdout))
}
