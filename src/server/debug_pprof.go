package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"expvar"
	"fmt"
	"net/http"
	"net/http/pprof"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/apimgr/gitignore/src/config"
	"github.com/go-chi/chi/v5"
)

// expvarOnce guards publishExpvars so the runtime variables are registered at
// most once per process. expvar.Publish panics on a duplicate name, and
// Server.New may run repeatedly (notably in tests).
var (
	expvarOnce  sync.Once
	expvarStart = time.Now()
)

// publishExpvars registers process runtime variables served at /debug/vars
// (AI.md PART 6 "expvar Registration"): process uptime, live goroutine count
// and a memory summary. Request/error counters are intentionally left to the
// Prometheus metrics subsystem (PART 20) to avoid double-instrumenting the
// request path.
func publishExpvars() {
	expvarOnce.Do(func() {
		expvar.Publish("uptime_seconds", expvar.Func(func() any {
			return time.Since(expvarStart).Seconds()
		}))
		expvar.Publish("goroutines", expvar.Func(func() any {
			return runtime.NumGoroutine()
		}))
		expvar.Publish("memory", expvar.Func(func() any {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			return map[string]uint64{
				"alloc":       m.Alloc,
				"total_alloc": m.TotalAlloc,
				"sys":         m.Sys,
				"heap_alloc":  m.HeapAlloc,
				"num_gc":      uint64(m.NumGC),
			}
		}))
	})
}

// registerDebugRoutes mounts the diagnostic endpoints on r. It is only invoked
// when the independent debug flag is set (--debug / DEBUG=true), never gated on
// application mode (AI.md PART 6). When debug is off these routes are never
// registered, so they return 404.
func (s *Server) registerDebugRoutes(r chi.Router) {
	publishExpvars()
	settings := config.DefaultConfig().Server.Debug
	if cfg := s.currentConfig(); cfg != nil {
		settings = cfg.Server.Debug
	}
	r.Route("/debug", func(debug chi.Router) {
		debug.Use(s.debugOperatorTokenMiddleware)
		if settings.RuntimeEndpoints {
			debug.Get("/routes", s.handleDebugRoutes)
			debug.Get("/config", s.handleDebugConfig)
			debug.Get("/templates", s.handleDebugTemplates)
			debug.Get("/cache", s.handleDebugCache)
			debug.Get("/db", s.handleDebugDB)
			debug.Get("/scheduler", s.handleDebugScheduler)
			debug.Get("/memory", s.handleDebugMemory)
			debug.Get("/goroutines", s.handleDebugGoroutines)
			debug.Get("/error500", s.handleDebugError500)
			debug.Handle("/vars", http.HandlerFunc(s.handleDebugVars))
		}
		if settings.Pprof {
			debug.HandleFunc("/pprof", pprof.Index)
			debug.HandleFunc("/pprof/", pprof.Index)
			debug.HandleFunc("/pprof/cmdline", s.handleDebugCmdline)
			debug.HandleFunc("/pprof/profile", pprof.Profile)
			debug.HandleFunc("/pprof/symbol", pprof.Symbol)
			debug.HandleFunc("/pprof/trace", pprof.Trace)
			debug.Handle("/pprof/heap", pprof.Handler("heap"))
			debug.Handle("/pprof/goroutine", pprof.Handler("goroutine"))
			debug.Handle("/pprof/allocs", pprof.Handler("allocs"))
			debug.Handle("/pprof/block", pprof.Handler("block"))
			debug.Handle("/pprof/mutex", pprof.Handler("mutex"))
			debug.Handle("/pprof/threadcreate", pprof.Handler("threadcreate"))
		}
	})
}

func (s *Server) debugOperatorTokenMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := parseBearerHeader(r.Header.Get("Authorization"))
		cfg := s.currentConfig()
		if !ok || cfg == nil || cfg.Server.Token == "" {
			s.sendBearerError(w, r)
			return
		}
		presented := sha256.Sum256([]byte(raw))
		configured := sha256.Sum256([]byte(cfg.Server.Token))
		if subtle.ConstantTimeCompare(presented[:], configured[:]) != 1 {
			s.sendBearerError(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleDebugCmdline(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = fmt.Fprint(w, filepath.Base(os.Args[0]))
	_, _ = w.Write([]byte{0})
}

func (s *Server) handleDebugVars(w http.ResponseWriter, _ *http.Request) {
	vars := make(map[string]json.RawMessage)
	expvar.Do(func(kv expvar.KeyValue) {
		if kv.Key == "cmdline" {
			encoded, _ := json.Marshal(filepath.Base(os.Args[0]))
			vars[kv.Key] = encoded
			return
		}
		if json.Valid([]byte(kv.Value.String())) {
			vars[kv.Key] = json.RawMessage(kv.Value.String())
		}
	})
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = encodeJSON(w, vars)
}
