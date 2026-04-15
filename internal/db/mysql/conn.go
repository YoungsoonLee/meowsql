// Package mysql is the MySQL collector: it produces the same target.ContextPack
// that the Postgres collector does, so downstream consumers (agent, report) do
// not need to know which dialect they are looking at.
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	_ "github.com/go-sql-driver/mysql"
)

type Collector struct {
	db     *sql.DB
	dbName string
}

// Open accepts either the go-sql-driver DSN ("user:pass@tcp(host:port)/db?...")
// or a URL form ("mysql://user:pass@host:port/db?..."). The URL form is
// translated for users who prefer URI-style DSNs.
func Open(ctx context.Context, dsn string) (*Collector, error) {
	translated, err := translateDSN(dsn)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("mysql", translated)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	var dbName string
	_ = db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&dbName)
	return &Collector{db: db, dbName: dbName}, nil
}

func (c *Collector) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

func (c *Collector) serverVersion(ctx context.Context) (string, error) {
	var v string
	err := c.db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&v)
	return v, err
}

// translateDSN converts "mysql://user:pass@host:port/db?params" to the DSN
// format the go-sql-driver expects. Non-URL input is passed through unchanged.
func translateDSN(dsn string) (string, error) {
	if !strings.HasPrefix(dsn, "mysql://") {
		return dsn, nil
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("parse mysql url: %w", err)
	}

	var user string
	if u.User != nil {
		user = u.User.Username()
		if pw, ok := u.User.Password(); ok {
			user += ":" + pw
		}
		user += "@"
	}

	host := u.Host
	if host == "" {
		host = "localhost:3306"
	}
	if !strings.Contains(host, ":") {
		host += ":3306"
	}

	db := strings.TrimPrefix(u.Path, "/")
	q := u.RawQuery

	out := fmt.Sprintf("%stcp(%s)/%s", user, host, db)
	if q != "" {
		out += "?" + q
	}
	return out, nil
}
