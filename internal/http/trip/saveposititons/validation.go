package saveposititons

import (
	"errors"
	"time"
)

func validateSavePositionRequest(body savePositionRequest, now time.Time) error {
	if body.Coordinates == nil {
		return errors.New("coordinates is required")
	}

	latitude := body.Coordinates.Latitude
	if latitude == nil {
		return errors.New("coordinates.latitude is required")
	}
	if *latitude < -90 || *latitude > 90 {
		return errors.New("coordinates.latitude must be between -90 and 90")
	}

	longitude := body.Coordinates.Longitude
	if longitude == nil {
		return errors.New("coordinates.longitude is required")
	}
	if *longitude < -180 || *longitude > 180 {
		return errors.New("coordinates.longitude must be between -180 and 180")
	}

	if body.RecordedAt == nil {
		return errors.New("recorded_at is required")
	}
	if body.RecordedAt.After(now.Add(time.Minute)) {
		return errors.New("recorded_at must be at most one minute in the future")
	}

	return nil
}
