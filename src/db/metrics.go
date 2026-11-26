package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync"
	"time"

	_ "github.com/tursodatabase/libsql-client-go/libsql"
)

type queryObserver func(operation, table string, duration time.Duration, err error)

var (
	observerMu        sync.RWMutex
	observer          queryObserver
	writeObserver     func(time.Duration, bool)
	metricsDriverMu   sync.Mutex
	metricsDriverMap  = map[string]string{}
	metricsDB         = registerMetricsDriver()
	cacheReaderDriver = registerCacheReaderDriver()
)

// SetWriteObserver installs the callback for bounded SQLite write wait metrics.
func SetWriteObserver(callback func(time.Duration, bool)) {
	observerMu.Lock()
	writeObserver = callback
	observerMu.Unlock()
}

func recordWriteWait(wait time.Duration, busy bool) {
	observerMu.RLock()
	callback := writeObserver
	observerMu.RUnlock()
	if callback != nil {
		callback(wait, busy)
	}
}

func registerMetricsDriver() string {
	return registerMetricsDriverFor("sqlite")
}

func registerMetricsDriverFor(driverName string) string {
	metricsDriverMu.Lock()
	defer metricsDriverMu.Unlock()
	if name := metricsDriverMap[driverName]; name != "" {
		return name
	}
	base, err := sql.Open(driverName, ":memory:")
	if err != nil {
		return driverName
	}
	driverImpl := base.Driver()
	_ = base.Close()
	name := driverName + "-metrics"
	sql.Register(name, &instrumentedDriver{base: driverImpl, prefix: driverName == "libsql"})
	metricsDriverMap[driverName] = name
	return name
}

func registerCacheReaderDriver() string {
	base, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return "sqlite"
	}
	driverImpl := base.Driver()
	_ = base.Close()
	const name = "sqlite-cache-metrics"
	sql.Register(name, &instrumentedDriver{base: driverImpl, cacheReader: true})
	return name
}

// SetQueryObserver installs the low-cardinality database metrics callback.
func SetQueryObserver(callback func(operation, table string, duration time.Duration, err error)) {
	observerMu.Lock()
	observer = callback
	observerMu.Unlock()
}

func observeQuery(query string, start time.Time, err error) {
	observerMu.RLock()
	callback := observer
	observerMu.RUnlock()
	if callback == nil {
		return
	}
	operation, table := queryLabels(query)
	callback(operation, table, time.Since(start), err)
}

func queryLabels(query string) (string, string) {
	fields := strings.Fields(strings.TrimSpace(query))
	if len(fields) == 0 {
		return "select", "unknown"
	}
	operation := strings.ToLower(fields[0])
	if operation == "with" {
		upper := strings.ToUpper(query)
		if strings.Contains(upper, "INSERT") {
			operation = "insert"
		} else if strings.Contains(upper, "UPDATE") {
			operation = "update"
		} else if strings.Contains(upper, "DELETE") {
			operation = "delete"
		} else {
			operation = "select"
		}
	}
	if operation == "replace" {
		operation = "insert"
	}
	if operation != "select" && operation != "insert" && operation != "update" && operation != "delete" {
		return "select", "unknown"
	}
	upper := strings.ToUpper(query)
	keywords := []string{"FROM", "INTO", "UPDATE", "JOIN"}
	for _, keyword := range keywords {
		index := strings.Index(upper, keyword+" ")
		if index < 0 {
			continue
		}
		rest := strings.Fields(query[index+len(keyword):])
		if len(rest) > 0 {
			table := strings.Trim(rest[0], "`\"[](),")
			if dot := strings.LastIndexByte(table, '.'); dot >= 0 {
				table = table[dot+1:]
			}
			if table != "" {
				return operation, strings.ToLower(table)
			}
		}
	}
	return operation, "unknown"
}

func QueryErrorType(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "timeout"
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "constraint") {
		if strings.Contains(message, "unique") || strings.Contains(message, "duplicate") {
			return "duplicate"
		}
		return "constraint"
	}
	if strings.Contains(message, "connection") || strings.Contains(message, "closed") || strings.Contains(message, "i/o") {
		return "connection"
	}
	return "other"
}

type instrumentedDriver struct {
	base        driver.Driver
	prefix      bool
	cacheReader bool
}

func (d *instrumentedDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.base.Open(name)
	if err != nil {
		return nil, err
	}
	return &instrumentedConn{Conn: conn, prefix: d.prefix, cacheReader: d.cacheReader}, nil
}

type instrumentedConn struct {
	driver.Conn
	prefix      bool
	cacheReader bool
	refresh     func(context.Context)
	txMu        sync.Mutex
	txState     *instrumentedTxState
}

type instrumentedTxState struct {
	wrote bool
}

type instrumentedTx struct {
	driver.Tx
	conn  *instrumentedConn
	state *instrumentedTxState
	ctx   context.Context
}

func (c *instrumentedConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *instrumentedConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	var tx driver.Tx
	var err error
	if beginner, ok := c.Conn.(driver.ConnBeginTx); ok {
		tx, err = beginner.BeginTx(ctx, opts)
	} else {
		//lint:ignore SA1019 Some drivers implement only the required legacy Begin method.
		tx, err = c.Conn.Begin()
	}
	if err != nil {
		return nil, err
	}
	state := &instrumentedTxState{}
	c.txMu.Lock()
	c.txState = state
	c.txMu.Unlock()
	return &instrumentedTx{Tx: tx, conn: c, state: state, ctx: ctx}, nil
}

func (c *instrumentedConn) noteWrite(ctx context.Context, query string) {
	if !c.prefix || !queryWrites(query) {
		return
	}
	c.txMu.Lock()
	if c.txState != nil {
		c.txState.wrote = true
		c.txMu.Unlock()
		return
	}
	c.txMu.Unlock()
	c.refreshCache(ctx)
}

func (c *instrumentedConn) finishTransaction(state *instrumentedTxState, committed bool, ctx context.Context) {
	c.txMu.Lock()
	if c.txState != state {
		c.txMu.Unlock()
		return
	}
	wrote := state.wrote
	c.txState = nil
	c.txMu.Unlock()
	if committed && wrote {
		c.refreshCache(ctx)
	}
}

func (c *instrumentedConn) refreshCache(ctx context.Context) {
	if c.refresh != nil {
		c.refresh(ctx)
		return
	}
	if localCache != nil {
		localCache.refreshAfterWrite(ctx)
	}
}

func (tx *instrumentedTx) Commit() error {
	err := tx.Tx.Commit()
	tx.conn.finishTransaction(tx.state, err == nil, tx.ctx)
	return err
}

func (tx *instrumentedTx) Rollback() error {
	err := tx.Tx.Rollback()
	tx.conn.finishTransaction(tx.state, false, tx.ctx)
	return err
}

func (c *instrumentedConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	start := time.Now()
	execer, ok := c.Conn.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	result, err := execer.ExecContext(ctx, mapServerIdentifiers(query, c.prefix), args)
	observeQuery(query, start, err)
	if err == nil {
		c.noteWrite(ctx, query)
	}
	return result, err
}

func (c *instrumentedConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if c.cacheReader && localCache != nil && localCache.isExpired() {
		if rows, err := localCache.freshRemoteQuery(ctx, query, args); err == nil {
			return rows, nil
		} else {
			return nil, err
		}
	}
	start := time.Now()
	queryer, ok := c.Conn.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	rows, err := queryer.QueryContext(ctx, mapServerIdentifiers(query, c.prefix), args)
	observeQuery(query, start, err)
	if err != nil && c.prefix && localCache != nil {
		if cached, cacheErr := localCache.fallbackQuery(ctx, query, args); cacheErr == nil {
			return cached, nil
		}
	}
	if err == nil && c.cacheReader && localCache != nil {
		rows = localCache.readThrough(rows, ctx, query, args)
	}
	return rows, err
}

func (c *instrumentedConn) Prepare(query string) (driver.Stmt, error) {
	mapped := mapServerIdentifiers(query, c.prefix)
	stmt, err := c.Conn.Prepare(mapped)
	if err != nil {
		return nil, err
	}
	return &instrumentedStmt{Stmt: stmt, query: query, prefix: c.prefix, cacheReader: c.cacheReader, conn: c}, nil
}

// serverObjectName is the single SQLite/libSQL identifier mapping function.
func serverObjectName(name string, prefix bool) string {
	if !prefix || strings.HasPrefix(name, "srv_") {
		return name
	}
	switch strings.ToLower(name) {
	case "rate_limits", "idx_rate_limits_window", "rate_limit_events", "idx_rate_limit_events_key_time",
		"audit_log", "idx_audit_timestamp", "idx_audit_category", "idx_audit_actor_ip", "idx_audit_target",
		"scheduler_tasks", "scheduler_history", "idx_scheduler_history_task", "idx_scheduler_history_started", "server_scheduler_state",
		"backups", "idx_backups_created", "ip_blocks", "idx_ip_blocks_expiry", "app_secrets", "idx_app_secrets_name", "app_secret_revocations", "pgp_key_metadata",
		"security_reports", "idx_security_reports_status", "security_thanks", "idx_security_thanks_disclosed_on",
		"idx_security_thanks_tracking_id", "owners", "api_tokens", "idx_api_tokens_primary", "idx_api_tokens_hash",
		"idx_api_tokens_prefix", "idx_api_tokens_owner", "idx_api_tokens_active", "idx_api_tokens_owner_name":
		return "srv_" + name
	default:
		return name
	}
}

// mapServerIdentifiers prefixes known table/index identifiers for libSQL while
// leaving string literals and comments untouched.
func mapServerIdentifiers(query string, prefix bool) string {
	if !prefix || query == "" {
		return query
	}
	var out strings.Builder
	out.Grow(len(query) + 16)
	for i := 0; i < len(query); {
		if query[i] == '\'' {
			start := i
			i++
			for i < len(query) {
				if query[i] == '\'' {
					i++
					if i < len(query) && query[i] == '\'' {
						i++
						continue
					}
					break
				}
				i++
			}
			out.WriteString(query[start:i])
			continue
		}
		if query[i] == '-' && i+1 < len(query) && query[i+1] == '-' {
			start := i
			for i < len(query) && query[i] != '\n' {
				i++
			}
			out.WriteString(query[start:i])
			continue
		}
		if query[i] == '/' && i+1 < len(query) && query[i+1] == '*' {
			start := i
			i += 2
			for i+1 < len(query) && (query[i] != '*' || query[i+1] != '/') {
				i++
			}
			if i+1 < len(query) {
				i += 2
			}
			out.WriteString(query[start:i])
			continue
		}
		if query[i] == '"' || query[i] == '`' || query[i] == '[' {
			open := query[i]
			close := open
			if open == '[' {
				close = ']'
			}
			start := i
			i++
			nameStart := i
			for i < len(query) && query[i] != close {
				i++
			}
			name := query[nameStart:i]
			if i < len(query) {
				i++
			}
			mapped := serverObjectName(name, prefix)
			out.WriteByte(open)
			out.WriteString(mapped)
			if i > start+len(name)+1 {
				out.WriteByte(close)
			}
			continue
		}
		if isSQLIdentifierStart(query[i]) {
			start := i
			i++
			for i < len(query) && isSQLIdentifierContinue(query[i]) {
				i++
			}
			out.WriteString(serverObjectName(query[start:i], prefix))
			continue
		}
		out.WriteByte(query[i])
		i++
	}
	return out.String()
}

func isSQLIdentifierStart(value byte) bool {
	return value == '_' || value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func isSQLIdentifierContinue(value byte) bool {
	return isSQLIdentifierStart(value) || value >= '0' && value <= '9'
}

func (c *instrumentedConn) CheckNamedValue(value *driver.NamedValue) error {
	if checker, ok := c.Conn.(driver.NamedValueChecker); ok {
		return checker.CheckNamedValue(value)
	}
	return driver.ErrSkip
}

type instrumentedStmt struct {
	driver.Stmt
	query       string
	prefix      bool
	cacheReader bool
	conn        *instrumentedConn
}

func (s *instrumentedStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	stmt, ok := s.Stmt.(driver.StmtExecContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	start := time.Now()
	result, err := stmt.ExecContext(ctx, args)
	observeQuery(s.query, start, err)
	if err == nil && s.conn != nil {
		s.conn.noteWrite(ctx, s.query)
	}
	return result, err
}

func (s *instrumentedStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if s.cacheReader && localCache != nil && localCache.isExpired() {
		rows, err := localCache.freshRemoteQuery(ctx, s.query, args)
		if err != nil {
			return nil, err
		}
		return rows, nil
	}
	stmt, ok := s.Stmt.(driver.StmtQueryContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	start := time.Now()
	rows, err := stmt.QueryContext(ctx, args)
	observeQuery(s.query, start, err)
	if err != nil && s.prefix && localCache != nil {
		if cached, cacheErr := localCache.fallbackQuery(ctx, s.query, args); cacheErr == nil {
			return cached, nil
		}
	}
	if err == nil && s.cacheReader && localCache != nil {
		rows = localCache.readThrough(rows, ctx, s.query, args)
	}
	return rows, err
}

func queryWrites(query string) bool {
	fields := strings.Fields(strings.TrimSpace(query))
	if len(fields) == 0 {
		return false
	}
	switch strings.ToLower(fields[0]) {
	case "insert", "update", "delete", "replace", "create", "alter", "drop":
		return true
	default:
		return false
	}
}
