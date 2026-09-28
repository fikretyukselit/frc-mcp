// Command frc-mcp is the FRC Model Context Protocol server and its tooling.
//
//	frc-mcp serve   [--transport stdio|http] [--index DIR] ...   (default)
//	frc-mcp index build --chunks F --symbols F --out SHARD
//	frc-mcp doctor  [--index DIR]
//	frc-mcp version
//
// Logs go to stderr only: stdout carries the stdio transport.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
)

// version is set by goreleaser via -ldflags "-X main.version=…".
var version = ""

func buildVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

func main() { os.Exit(run()) }

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve(ctx, log, args)
	case "index":
		err = indexCmd(ctx, args)
	case "doctor":
		err = doctor(ctx, args)
	case "eval":
		err = evalCmd(ctx, args)
	case "verify":
		err = verifyCmd(ctx, args)
	case "sync":
		err = syncCmd(ctx, args)
	case "version":
		fmt.Println(buildVersion())
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "frc-mcp: unknown command %q\n\n", cmd)
		usage()
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "frc-mcp:", err)
		return 1
	}
	return 0
}

func usage() {
	fmt.Fprint(os.Stderr, `frc-mcp — version-correct FRC knowledge for AI coding agents

Usage:
  frc-mcp [serve] [flags]        run the MCP server (stdio by default)
  frc-mcp index build [flags]    build a shard from JSONL chunks/symbols
  frc-mcp sync [flags]           download / update the signed index
  frc-mcp index run [flags]      ingest data/sources.yaml into shards
  frc-mcp index publish [flags]  package + sign shards for distribution
  frc-mcp index keygen           create an index signing key pair
  frc-mcp doctor [flags]         check the local index and measure latency
  frc-mcp eval [flags]           retrieval metrics on eval/queries.jsonl
  frc-mcp verify [flags] DIR     check a robot project's Java code against its season's API
  frc-mcp version                print the version

Run "frc-mcp <command> -h" for flags.
`)
}
