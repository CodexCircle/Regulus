package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// Rebind rewrites the ? placeholders of a query written for SQLite into the
// $1, $2, ... that PostgreSQL wants. A ? inside a quoted string or identifier
// is left alone. The rest of the code writes its queries once, with ?.
func Rebind(query string) string {
	if !strings.Contains(query, "?") {
		return query
	}
	var b strings.Builder
	b.Grow(len(query) + 8)
	n := 0
	var quote byte
	for i := 0; i < len(query); i++ {
		c := query[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
			b.WriteByte(c)
		case c == '\'' || c == '"':
			quote = c
			b.WriteByte(c)
		case c == '?':
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// openPostgres returns a pool of connections that rebind the placeholders.
func openPostgres(cfg Config) (*sql.DB, error) {
	pc, err := pgx.ParseConfig(cfg.URL())
	if err != nil {
		// The message of the parser can quote the URL: do not pass it on.
		return nil, fmt.Errorf("invalid PostgreSQL settings (check host, port and sslmode)")
	}
	db := sql.OpenDB(rebindConnector{inner: stdlib.GetConnector(*pc)})
	// A pool with sensible limits. A connection is recycled now and then so a
	// restart of PostgreSQL, or a firewall that drops idle connections, shows
	// up as a retried connection rather than as errors.
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)
	return db, nil
}

type rebindConnector struct{ inner driver.Connector }

func (c rebindConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &rebindConn{inner: conn}, nil
}

func (c rebindConnector) Driver() driver.Driver { return c.inner.Driver() }

// rebindConn forwards everything to the pgx connection, changing the query
// text on the way.
type rebindConn struct{ inner driver.Conn }

func (c *rebindConn) Prepare(query string) (driver.Stmt, error) {
	return c.inner.Prepare(Rebind(query))
}

func (c *rebindConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	return c.inner.(driver.ConnPrepareContext).PrepareContext(ctx, Rebind(query))
}

func (c *rebindConn) Close() error { return c.inner.Close() }

func (c *rebindConn) Begin() (driver.Tx, error) { return c.inner.Begin() } //nolint:staticcheck // required by driver.Conn

func (c *rebindConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.inner.(driver.ConnBeginTx).BeginTx(ctx, opts)
}

func (c *rebindConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	// Without arguments the text may hold several statements (a migration):
	// leave it exactly as it is.
	if len(args) > 0 {
		query = Rebind(query)
	}
	return c.inner.(driver.ExecerContext).ExecContext(ctx, query, args)
}

func (c *rebindConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if len(args) > 0 {
		query = Rebind(query)
	}
	return c.inner.(driver.QueryerContext).QueryContext(ctx, query, args)
}

// The optional interfaces below are forwarded when the pgx connection has
// them, and answer "nothing to do" when it does not.

func (c *rebindConn) Ping(ctx context.Context) error {
	if p, ok := c.inner.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}

func (c *rebindConn) ResetSession(ctx context.Context) error {
	if r, ok := c.inner.(driver.SessionResetter); ok {
		return r.ResetSession(ctx)
	}
	return nil
}

func (c *rebindConn) IsValid() bool {
	if v, ok := c.inner.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}

func (c *rebindConn) CheckNamedValue(nv *driver.NamedValue) error {
	if n, ok := c.inner.(driver.NamedValueChecker); ok {
		return n.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}
