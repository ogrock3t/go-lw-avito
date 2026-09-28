package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TxManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

type Manager struct {
	pool *pgxpool.Pool
}

func NewTxManager(pool *pgxpool.Pool) *Manager {
	return &Manager{
		pool: pool,
	}
}

type txKey struct{}

func (m *Manager) Do(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	if tx := txFromContext(ctx); tx != nil {
		if fn == nil {
			return nil
		}

		return fn(ctx)
	}

	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.ReadCommitted,
	})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	txCtx := context.WithValue(ctx, txKey{}, tx)

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}

		if err != nil {
			if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				err = fmt.Errorf("rollback transaction: %v: original error: %w", rollbackErr, err)
			}
			return
		}

		if commitErr := tx.Commit(ctx); commitErr != nil {
			err = fmt.Errorf("commit transaction: %w", commitErr)
		}
	}()

	if fn != nil {
		err = fn(txCtx)
	}

	return err
}

func txFromContext(ctx context.Context) pgx.Tx {
	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	if !ok {
		return nil
	}

	return tx
}

type QueryExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func executorFromContext(ctx context.Context, pool *pgxpool.Pool) QueryExecutor {
	if tx := txFromContext(ctx); tx != nil {
		return tx
	}

	return pool
}
