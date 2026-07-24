// Command bestiary-web serves an OFFLINE, read-only web view of the bestiary catalog.
//
// It reads only the in-process static registry (bestiary.Entities()/StaticModels()) and,
// optionally, a read-only SQLite cache — it NEVER touches the network at serve time. The
// browser fetches the vendored, same-origin datastar.js client and (per the RQ1 ruling)
// two webfonts from a CDN; those are the browser's fetches, not this server's. The server
// process makes no outbound requests.
//
// Usage:
//
//	bestiary-web [--addr :8080] [--cache /path/to/cache.db]
//
// With no --cache, the server runs static-only. With --cache pointing at an existing
// bestiary sync cache, cached model rows are counted into the landing page; the cache is
// opened read-only and never written.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/dayvidpham/bestiary"
)

func main() {
	addr := flag.String("addr", ":8080", "TCP address to listen on")
	cachePath := flag.String("cache", "", "path to an existing bestiary SQLite cache to read (optional; static-only if empty)")
	flag.Parse()

	if err := run(*addr, *cachePath); err != nil {
		fmt.Fprintln(os.Stderr, "bestiary-web:", err)
		os.Exit(1)
	}
}

func run(addr, cachePath string) error {
	var cache *bestiary.Store
	if cachePath != "" {
		// Only open a cache that already exists — never create one as a serve-time side
		// effect (that would be a silent write to a path the operator did not intend).
		if _, err := os.Stat(cachePath); err != nil {
			return fmt.Errorf("cache %q is not readable: %w (omit --cache to run static-only)", cachePath, err)
		}
		st, err := bestiary.OpenStore(cachePath)
		if err != nil {
			return fmt.Errorf("open cache %q: %w", cachePath, err)
		}
		defer st.Close()
		cache = st
	}

	srv, err := NewServer(bestiary.Entities(), cache)
	if err != nil {
		return err
	}

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("bestiary-web: serving %d entities on %s (offline, read-only)", len(srv.entities), addr)
	return httpSrv.ListenAndServe()
}
