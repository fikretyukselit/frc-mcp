package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/mcpserver"
	"github.com/fikretyukselit/frc-mcp/internal/retrieve"
)

func serve(ctx context.Context, log *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	transport := fs.String("transport", "stdio", "stdio | http")
	dir := fs.String("index", index.DefaultDir(), "directory containing *.sqlite shards")
	addr := fs.String("addr", "127.0.0.1:7424", "listen address for --transport http")
	season := fs.String("default-season", "", "season used when nothing pins one (default: newest stable season in the index)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	start := time.Now()
	shards, errs := index.OpenDir(ctx, *dir)
	for _, err := range errs {
		log.Warn("shard skipped", "err", err)
	}
	var engine *retrieve.Engine
	if len(shards) > 0 {
		engine = retrieve.New(shards, retrieve.Options{DefaultSeason: *season})
		log.Info("index loaded", "dir", *dir, "shards", len(shards), "default_season", engine.DefaultSeason(),
			"digest", engine.Digest(), "open_ms", time.Since(start).Milliseconds())
	} else {
		log.Warn("no index shards found; tools will report status=syncing", "dir", *dir)
	}
	defer func() {
		for _, s := range shards {
			s.Close()
		}
	}()
	srv := mcpserver.New(engine, mcpserver.Options{Version: buildVersion()})

	switch *transport {
	case "stdio":
		err := srv.MCP().Run(ctx, &mcp.StdioTransport{})
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	case "http":
		return serveHTTP(ctx, log, srv, *addr)
	default:
		return fmt.Errorf("unknown transport %q (want stdio or http)", *transport)
	}
}

// serveHTTP runs the stateless Streamable HTTP transport (hosted profile).
// Non-loopback binds require FRC_MCP_TOKEN (bearer auth); DNS-rebinding and
// cross-origin protection are always on.
func serveHTTP(ctx context.Context, log *slog.Logger, srv *mcpserver.Server, addr string) error {
	token := os.Getenv("FRC_MCP_TOKEN")
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); (ip == nil || !ip.IsLoopback()) && host != "localhost" && token == "" {
		return fmt.Errorf("refusing to listen on non-loopback %s without FRC_MCP_TOKEN set", addr)
	}
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv.MCP() }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true, PropagateRequestCancellation: true, MaxRequestBodyBytes: 1 << 20,
	})
	handler := http.NewCrossOriginProtection().Handler(h)
	if token != "" {
		handler = bearer(token, handler)
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok\n")) })
	hs := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second, MaxHeaderBytes: 64 << 10}
	go func() {
		<-ctx.Done()
		// ctx is already canceled here; shutdown needs its own deadline.
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = hs.Shutdown(sctx)
	}()
	log.Info("listening", "addr", "http://"+addr+"/mcp", "auth", token != "")
	if err := hs.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func bearer(token string, next http.Handler) http.Handler {
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(strings.TrimSpace(r.Header.Get("Authorization")))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="frc-mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
