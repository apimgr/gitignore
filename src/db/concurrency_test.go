package db

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestSQLiteReadersAndWritersDoNotReturnBusyErrors(t *testing.T) {
	t.Setenv("DATABASE_DIR", "")
	if err := Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	if _, err := conn.Exec("CREATE TABLE concurrency_probe (id INTEGER PRIMARY KEY, writer INTEGER NOT NULL)"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	errors := make(chan error, 40)
	var workers sync.WaitGroup
	for writer := 0; writer < 20; writer++ {
		workers.Add(1)
		go func(id int) {
			defer workers.Done()
			for ctx.Err() == nil {
				if err := Write(ctx, func(tx *sql.Tx) error {
					_, err := tx.Exec("INSERT INTO concurrency_probe(writer) VALUES (?)", id)
					return err
				}); err != nil {
					if ctx.Err() == nil {
						errors <- fmt.Errorf("writer %d: %w", id, err)
					}
					return
				}
			}
		}(writer)
	}
	for reader := 0; reader < 20; reader++ {
		workers.Add(1)
		go func(id int) {
			defer workers.Done()
			for ctx.Err() == nil {
				if err := Read(ctx, func(queryCtx context.Context, reader *sql.DB) error {
					var count int
					return reader.QueryRowContext(queryCtx, "SELECT COUNT(*) FROM concurrency_probe").Scan(&count)
				}); err != nil {
					if ctx.Err() == nil {
						errors <- fmt.Errorf("reader %d: %w", id, err)
					}
					return
				}
			}
		}(reader)
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}
