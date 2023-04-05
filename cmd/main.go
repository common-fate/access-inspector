package main

import (
	"os"

	"github.com/common-fate/access-inspector/cmd/command"
	"github.com/common-fate/clio"
	"github.com/common-fate/clio/clierr"
	"github.com/urfave/cli/v2"
)

func main() {
	clio.SetLevelFromEnv("CF_LOG")

	app := &cli.App{
		Name:      "access-inspector",
		Writer:    os.Stderr,
		Usage:     "https://commonfate.io",
		UsageText: "access-inspector [options] [command]",
		Commands:  []*cli.Command{&command.Scan, &command.Analyze, &command.DumpRequests},
	}
	err := app.Run(os.Args)
	if err != nil {
		// if the error is an instance of clierr.PrintCLIErrorer then print the error accordingly
		if cliError, ok := err.(clierr.PrintCLIErrorer); ok {
			cliError.PrintCLIError()
		} else {
			clio.Error(err.Error())
		}
		os.Exit(1)
	}
}
