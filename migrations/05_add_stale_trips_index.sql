-- +goose Up
CREATE INDEX trips_active_last_position_idx
ON trips ((COALESCE(last_position_at, started_at)))
WHERE status = 'active';

-- +goose Down
DROP INDEX trips_active_last_position_idx;