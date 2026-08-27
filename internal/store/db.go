package store

import (
	"database/sql"
	"strings"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite" // driver "sqlite" (pure-Go, tanpa CGO)
)

// dbConn membungkus sqlx.DB. Lamund memakai SQLite sebagai satu-satunya backend
// (single-binary, zero-config). Query ditulis dengan placeholder '?'.
type dbConn struct {
	sx *sqlx.DB
}

func (c *dbConn) Exec(q string, args ...any) (sql.Result, error) { return c.sx.Exec(q, args...) }
func (c *dbConn) Query(q string, args ...any) (*sql.Rows, error) { return c.sx.Query(q, args...) }
func (c *dbConn) QueryRow(q string, args ...any) *sql.Row        { return c.sx.QueryRow(q, args...) }
func (c *dbConn) Begin() (*dbTx, error) {
	tx, err := c.sx.Beginx()
	if err != nil {
		return nil, err
	}
	return &dbTx{tx: tx}, nil
}
func (c *dbConn) Close() error { return c.sx.Close() }
func (c *dbConn) Ping() error  { return c.sx.Ping() }

// nowExpr = ekspresi timestamp "sekarang" (created_at/finished_at bertipe TEXT).
func (c *dbConn) nowExpr() string { return "datetime('now')" }

// insertID menjalankan INSERT lalu mengembalikan id baris baru (SQLite LastInsertId).
func (c *dbConn) insertID(q string, args ...any) (int64, error) {
	res, err := c.Exec(q, args...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// dbTx = transaksi ter-wrap (paralel dbConn).
type dbTx struct {
	tx *sqlx.Tx
}

func (t *dbTx) Exec(q string, args ...any) (sql.Result, error) { return t.tx.Exec(q, args...) }
func (t *dbTx) Query(q string, args ...any) (*sql.Rows, error) { return t.tx.Query(q, args...) }
func (t *dbTx) QueryRow(q string, args ...any) *sql.Row        { return t.tx.QueryRow(q, args...) }
func (t *dbTx) Commit() error                                  { return t.tx.Commit() }
func (t *dbTx) Rollback() error                                { return t.tx.Rollback() }
func (t *dbTx) insertID(q string, args ...any) (int64, error) {
	res, err := t.Exec(q, args...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// openDB membuka koneksi SQLite (path file) dengan busy_timeout + WAL.
func openDB(dsn string) (*dbConn, error) {
	sx, err := sqlx.Open("sqlite", dsn+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	return &dbConn{sx: sx}, nil
}

// isUniqueErr mendeteksi pelanggaran UNIQUE SQLite ("UNIQUE constraint failed").
func isUniqueErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE")
}
