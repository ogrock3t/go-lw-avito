package create

import "github.com/google/uuid"

// ValidationError describes an invalid field in a create trip request.
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return e.Field + " " + e.Reason
}

func newValidationError(field, reason string) error {
	return &ValidationError{
		Field:  field,
		Reason: reason,
	}
}

func validateCreateTripRequest(body createTripRequest) error {
	if body.UserID == nil {
		return newValidationError("user_id", "is required")
	}

	if *body.UserID == uuid.Nil {
		return newValidationError("user_id", "must be non-empty UUID")
	}

	if body.DriverID == nil {
		return newValidationError("driver_id", "is required")
	}

	if *body.DriverID == uuid.Nil {
		return newValidationError("driver_id", "must be non-empty UUID")
	}

	if body.StartPoint == nil {
		return newValidationError("start_point", "is required")
	}

	if err := validateCoordinatesRequest("start_point", *body.StartPoint); err != nil {
		return err
	}

	if body.EndPoint == nil {
		return newValidationError("end_point", "is required")
	}

	if err := validateCoordinatesRequest("end_point", *body.EndPoint); err != nil {
		return err
	}

	if body.Price == nil {
		return newValidationError("price", "is required")
	}

	if *body.Price < 0 {
		return newValidationError("price", "must be non-negative")
	}

	return nil
}

func validateCoordinatesRequest(name string, coordinates coordinatesRequest) error {
	if coordinates.Latitude == nil {
		return newValidationError(name+".latitude", "is required")
	}

	if !validLatitude(*coordinates.Latitude) {
		return newValidationError(name+".latitude", "must be between -90 and 90")
	}

	if coordinates.Longitude == nil {
		return newValidationError(name+".longitude", "is required")
	}

	if !validLongitude(*coordinates.Longitude) {
		return newValidationError(name+".longitude", "must be between -180 and 180")
	}

	return nil
}

func validLatitude(value float64) bool {
	return value >= -90 && value <= 90
}

func validLongitude(value float64) bool {
	return value >= -180 && value <= 180
}
