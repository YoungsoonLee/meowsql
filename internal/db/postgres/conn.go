package postgres

import (
	"context"

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
