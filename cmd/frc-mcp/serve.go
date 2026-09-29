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
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/fikretyukselit/frc-mcp/internal/dist"
	"github.com/fikretyukselit/frc-mcp/internal/hosted"
	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/mcpserver"
)

func serve(ctx context.Context, log *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	transport := fs.String("transport", "stdio", "stdio | http")
	dir := fs.String("index", index.DefaultDir(), "directory containing *.sqlite shards")
	addr := fs.String("addr", "127.0.0.1:7424", "listen address for --transport http")
	season := fs.String("default-season", "", "season used when nothing pins one (default: newest stable season in the index)")
	noDense := fs.Bool("no-dense", false, "disable dense (vector) retrieval")
	offline := fs.Bool("offline", false, "never contact the network (no index sync)")
	syncURL := fs.String("sync-url", dist.DefaultSyncURL, "signed index channel to sync from")
	syncEvery := fs.Duration("sync-every", 6*time.Hour, "index sync interval")
	public := fs.Bool("public", false, "with --transport http, serve anonymous clients on a non-loopback address (per-client rate limit, concurrency cap) instead of requiring FRC_MCP_TOKEN")
	rate := fs.Float64("rate", 1, "public mode: sustained requests per second per client")
	burst := fs.Int("burst", 30, "public mode: request burst per client")
	maxInFlight := fs.Int("max-inflight", 64, "public mode: concurrent requests across all clients")
	trustProxy := fs.String("trust-proxy", "", "comma-separated CIDRs of the reverse proxy whose X-Forwarded-For is trusted (e.g. 172.16.0.0/12)")
	metricsAddr := fs.String("metrics-addr", "", "serve Prometheus metrics (aggregate counters only) on this address, e.g. 127.0.0.1:9464; off by default")
	unlicensed := fs.Bool("include-unlicensed", false, "with --transport http, also serve shards whose content has no redistribution license (LicenseRef-*); only with the vendors' permission")
	if err := fs.Parse(args); err != nil {
		return err
	}
	// Answer tools/list immediately; load shards, model and vector layers in
	// the background (tools report status=syncing until ready). This keeps
	// cold start far below client startup timeouts on any index size.
	cwd, _ := os.Getwd()
	srv := mcpserver.New(nil, mcpserver.Options{Version: buildVersion(), ProjectRoot: cwd, NoFilesystem: *transport == "http"})
	var mu sync.Mutex
	var current []*index.Reader
	load := func(reason string) {
		start := time.Now()
		// Over HTTP others connect, so serving is redistribution: leave out
		// unlicensed shards unless explicitly allowed. stdio serves the
		// user's own local index.
		engine, sh := openEngineWith(ctx, log, *dir, *season, !*noDense, *transport != "http" || *unlicensed)
		if engine == nil {
			log.Warn("no index shards found; tools report status=syncing", "dir", *dir, "hint", "run `frc-mcp sync`")
			return
		}
		srv.SetEngine(engine)
		mu.Lock()
		old := current
		current = sh
		mu.Unlock()
		// Give in-flight requests on the previous engine time to finish.
		time.AfterFunc(2*time.Minute, func() {
			for _, s := range old {
				s.Close()
			}
		})
		log.Info("index loaded", "reason", reason, "dir", *dir, "shards", len(sh), "default_season", engine.DefaultSeason(),
			"dense", engine.Dense(), "digest", engine.Digest(), "load_ms", time.Since(start).Milliseconds())
	}
	go func() {
		load("startup")
		if *offline {
			return
		}
		kr, err := dist.TrustedKeyring("")
		if err != nil || len(kr) == 0 {
			log.Info("index sync disabled: no trusted signing keys configured (use `frc-mcp sync --trusted-key` for a mirror)")
			return
		}
		client, err := syncClient(*syncURL, false)
		if err != nil {
			log.Warn("index sync disabled", "err", err)
			return
		}
		for {
			res, err := dist.Sync(ctx, dist.SyncOptions{BaseURL: *syncURL, Dir: *dir, Keyring: kr, Client: client})
			switch {
			case err != nil:
				log.Warn("index sync failed; serving the installed index", "err", err)
			case res.Updated:
				log.Info("index synced", "serial", res.Serial, "files", res.Downloaded, "bytes", res.Bytes)
				load("sync")
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(*syncEvery):
			}
		}
	}()
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		for _, s := range current {
			s.Close()
		}
	}()

	switch *transport {
	case "stdio":
		err := srv.MCP().Run(ctx, &mcp.StdioTransport{})
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	case "http":
		var proxies []netip.Prefix
		for _, c := range strings.Split(*trustProxy, ",") {
			if c = strings.TrimSpace(c); c == "" {
				continue
			}
			p, err := netip.ParsePrefix(c)
			if err != nil {
				return fmt.Errorf("--trust-proxy %q: %w", c, err)
			}
			proxies = append(proxies, p)
		}
		return serveHTTP(ctx, log, srv, httpOptions{addr: *addr, public: *public, metricsAddr: *metricsAddr,
			hosted: hosted.Options{Rate: *rate, Burst: *burst, MaxInFlight: *maxInFlight, TrustedProxies: proxies}})
	default:
		return fmt.Errorf("unknown transport %q (want stdio or http)", *transport)
	}
}

type httpOptions struct {
	addr, metricsAddr string
	public            bool
	hosted            hosted.Options
}

// serveHTTP runs the stateless Streamable HTTP transport (hosted profile).
// Non-loopback binds need FRC_MCP_TOKEN (bearer auth) or --public (anonymous
// clients, rate-limited per client). DNS-rebinding and cross-origin
// protection are always on.
func serveHTTP(ctx context.Context, log *slog.Logger, srv *mcpserver.Server, o httpOptions) error {
	token := os.Getenv("FRC_MCP_TOKEN")
	host, _, err := net.SplitHostPort(o.addr)
	if err != nil {
		return err
	}
	loopback := host == "localhost"
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		loopback = true
	}
	if !loopback && token == "" && !o.public {
		return fmt.Errorf("refusing to listen on non-loopback %s without FRC_MCP_TOKEN or --public", o.addr)
	}
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv.MCP() }, &mcp.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true, PropagateRequestCancellation: true, MaxRequestBodyBytes: 1 << 20,
	})
	handler := http.NewCrossOriginProtection().Handler(h)
	if token != "" && !o.public {
		handler = bearer(token, handler)
	}
	mw := hosted.New(o.hosted)
	if o.public || !loopback {
		handler = mw.Wrap(handler)
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !srv.Ready() {
			http.Error(w, "index not loaded", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ready\n"))
	})
	hs := &http.Server{Addr: o.addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second, MaxHeaderBytes: 64 << 10}
	servers := []*http.Server{hs}
	if o.metricsAddr != "" {
		mh, _, err := net.SplitHostPort(o.metricsAddr)
		if err != nil {
			return fmt.Errorf("--metrics-addr: %w", err)
		}
		a, perr := netip.ParseAddr(mh)
		switch {
		case mh == "localhost" || (perr == nil && (a.IsLoopback() || a.IsPrivate())):
		case perr == nil && a.IsUnspecified():
			log.Warn("metrics listen on all interfaces; never publish this port (use it inside a container network only)", "addr", o.metricsAddr)
		default:
			return fmt.Errorf("--metrics-addr %s must be a loopback, private or container-internal address (metrics are for the operator)", o.metricsAddr)
		}
		ms := &http.Server{Addr: o.metricsAddr, Handler: mw.Metrics, ReadHeaderTimeout: 5 * time.Second}
		servers = append(servers, ms)
		go func() {
			log.Info("metrics", "addr", "http://"+o.metricsAddr+"/metrics")
			if err := ms.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("metrics server", "err", err)
			}
		}()
	}
	go func() {
		<-ctx.Done()
		// ctx is already canceled here; shutdown needs its own deadline.
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		for _, s := range servers {
			_ = s.Shutdown(sctx)
		}
	}()
	log.Info("listening", "addr", "http://"+o.addr+"/mcp", "auth", token != "" && !o.public, "public", o.public)
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
