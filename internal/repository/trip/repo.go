package trip

import (
	"context"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ogrock3t/go-lw-avito/internal/infra/postgres/transaction"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{
		pool: pool,
	}
}

var psql = sq.StatementBuilder.PlaceholderFormat(sq.Dollar)

func (r *Repository) executor(ctx context.Context) transaction.QueryExecutor {
	return transaction.ExecutorFromContext(ctx, r.pool)
}
