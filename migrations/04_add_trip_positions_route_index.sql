-- +goose Up
CREATE INDEX trip_positions_trip_recorded_idx
ON trip_positions (trip_id, recorded_at, id);

-- +goose Down
DROP INDEX trip_positions_trip_recorded_idx;