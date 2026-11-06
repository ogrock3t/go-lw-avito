package health

import "context"

type databasePool interface {
	Ping(ctx context.Context) error
}
