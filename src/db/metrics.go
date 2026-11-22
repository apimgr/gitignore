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
	observerMu       sync.RWMutex
	observer         queryObserver
	metricsDriverMu  sync.Mutex
	metricsDriverMap = map[string]string{}
	metricsDB        = registerMetricsDriver()
)

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
	sql.Register(name, &instrumentedDriver{base: driverImpl})
	metricsDriverMap[driverName] = name
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

type instrumentedDriver struct{ base driver.Driver }

func (d *instrumentedDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.base.Open(name)
	if err != nil {
		return nil, err
	}
	return &instrumentedConn{Conn: conn}, nil
}

type instrumentedConn struct{ driver.Conn }

func (c *instrumentedConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	start := time.Now()
	execer, ok := c.Conn.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	result, err := execer.ExecContext(ctx, query, args)
	observeQuery(query, start, err)
	return result, err
}

func (c *instrumentedConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	start := time.Now()
	queryer, ok := c.Conn.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	rows, err := queryer.QueryContext(ctx, query, args)
	observeQuery(query, start, err)
	return rows, err
}

func (c *instrumentedConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.Conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	return &instrumentedStmt{Stmt: stmt, query: query}, nil
}

func (c *instrumentedConn) CheckNamedValue(value *driver.NamedValue) error {
	if checker, ok := c.Conn.(driver.NamedValueChecker); ok {
		return checker.CheckNamedValue(value)
	}
	return driver.ErrSkip
}

type instrumentedStmt struct {
	driver.Stmt
	query string
}

func (s *instrumentedStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	stmt, ok := s.Stmt.(driver.StmtExecContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	start := time.Now()
	result, err := stmt.ExecContext(ctx, args)
	observeQuery(s.query, start, err)
	return result, err
}

func (s *instrumentedStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	stmt, ok := s.Stmt.(driver.StmtQueryContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	start := time.Now()
	rows, err := stmt.QueryContext(ctx, args)
	observeQuery(s.query, start, err)
	return rows, err
}
