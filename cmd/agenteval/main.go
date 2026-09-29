// Command agenteval runs the agent-level eval (docs/benchmarks.md, M4): the
// same coding agent solves robot-code tasks with and without frc-mcp and
// each result is compiled against the season's GradleRIO classpath.
//
//	agenteval compile --season 2027 [--vendordep REVLib …] DIR
//	agenteval references [--tasks eval/tasks]
//	agenteval run --model sonnet [--trials 1] [--only id,…] [--conditions mcp,baseline] --out FILE
//	agenteval report FILE…
//
// It is a development tool: it is not part of the released binary.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
)

func main() { os.Exit(run()) }

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if len(os.Args) < 2 {
		usage()
		return 2
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	var err error
	switch os.Args[1] {
	case "compile":
		err = compileCmd(ctx, os.Args[2:])
	case "references":
		err = referencesCmd(ctx, os.Args[2:], log)
	case "run":
		err = runCmd(ctx, os.Args[2:], log)
	case "report":
		err = reportCmd(os.Args[2:])
	default:
		usage()
		return 2
	}
	if err != nil {
		var fail failure
		if errors.As(err, &fail) {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Fprintln(os.Stderr, "agenteval:", err)
		return 1
	}
	return 0
}

// failure is a check that ran and failed (exit 1 without "agenteval:").
type failure string

func (f failure) Error() string { return string(f) }

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  agenteval compile --season 2027 [--vendordep NAME …] DIR   compile a robot project like GradleRIO's compileJava
  agenteval references [--tasks eval/tasks]                  every task's reference solution must compile
  agenteval run --model M --out FILE [--trials N] [--only a,b] [--conditions mcp,baseline]
  agenteval report FILE…                                     compile-pass rates per condition`)
}
