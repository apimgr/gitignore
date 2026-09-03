package server

import (
	"expvar"
	"net/http/pprof"
	"runtime"
	"sync"
	"time"

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

	// Custom debug endpoints.
	r.Get("/debug/routes", s.handleDebugRoutes)
	r.Get("/debug/config", s.handleDebugConfig)
	r.Get("/debug/templates", s.handleDebugTemplates)

	// net/http/pprof profiles (AI.md PART 6 "pprof Endpoints").
	r.HandleFunc("/debug/pprof/", pprof.Index)
	r.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	r.HandleFunc("/debug/pprof/profile", pprof.Profile)
	r.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	r.HandleFunc("/debug/pprof/trace", pprof.Trace)
	r.Handle("/debug/pprof/heap", pprof.Handler("heap"))
	r.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
	r.Handle("/debug/pprof/allocs", pprof.Handler("allocs"))
	r.Handle("/debug/pprof/block", pprof.Handler("block"))
	r.Handle("/debug/pprof/mutex", pprof.Handler("mutex"))
	r.Handle("/debug/pprof/threadcreate", pprof.Handler("threadcreate"))

	// expvar runtime variables (AI.md PART 6 "/debug/vars").
	r.Handle("/debug/vars", expvar.Handler())
}
