package library

import (
	"context"
	"database/sql"
)

// txStmts prepares each statement once in a transaction and runs it again from there (closed with the transaction).
// A loop over a post's tags ran the same few statements for every tag, each parsed again: with a booru's 45 tags, the
// parsing was a third of a download's database work (2026-10-28, the user: optimize "for speed and system
// resources").
type txStmts struct {
	ctx context.Context
	tx  *sql.Tx
	m   map[string]*sql.Stmt
}

func newTxStmts(ctx context.Context, tx *sql.Tx) *txStmts {
	return &txStmts{ctx: ctx, tx: tx, m: map[string]*sql.Stmt{}}
}

func (s *txStmts) prepare(query string) (*sql.Stmt, error) {
	if st, ok := s.m[query]; ok {
		return st, nil
	}
	st, err := s.tx.PrepareContext(s.ctx, query)
	if err != nil {
		return nil, err
	}
	s.m[query] = st
	return st, nil
}

// rowScanner is a *sql.Row - or the error preparing its statement, reported by Scan as QueryRowContext does.
type rowScanner interface{ Scan(dest ...any) error }

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

func (s *txStmts) QueryRow(query string, args ...any) rowScanner {
	st, err := s.prepare(query)
	if err != nil {
		return errRow{err}
	}
	return st.QueryRowContext(s.ctx, args...)
}

func (s *txStmts) Exec(query string, args ...any) (sql.Result, error) {
	st, err := s.prepare(query)
	if err != nil {
		return nil, err
	}
	return st.ExecContext(s.ctx, args...)
}
