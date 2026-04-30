package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type Collector struct {
	conn *pgx.Conn
}

func Open(ctx context.Context, dsn string) (*Collector, error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, err
	}
	return &Collector{conn: conn}, nil
}

func (c *Collector) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close(context.Background())
}

func (c *Collector) serverVersion(ctx context.Context) (string, error) {
	var v string
	err := c.conn.QueryRow(ctx, "SHOW server_version").Scan(&v)
	return v, err
}

// ApplySafetySettings sets session-level safety parameters on the connection.
// lock_timeout prevents schema queries from blocking behind long-running
// transactions that hold table locks.
// Errors are non-fatal — the caller may choose to warn or ignore.
func (c *Collector) ApplySafetySettings(ctx context.Context, lockTimeout time.Duration) error {
	if lockTimeout <= 0 {
		lockTimeout = 5 * time.Second
	}
	_, err := c.conn.Exec(ctx,
		fmt.Sprintf("SET lock_timeout = '%dms'", lockTimeout.Milliseconds()),
	)
	return err
}
