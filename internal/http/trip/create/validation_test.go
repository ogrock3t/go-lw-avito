package create

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

const reasonRequired = "is required"

func TestValidateCreateTripRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		changeBody func(*createTripRequest)
		wantField  string
		wantReason string
	}{
		{
			name: "valid request",
		},
		{
			name: "user id is required",
			changeBody: func(body *createTripRequest) {
				body.UserID = nil
			},
			wantField:  "user_id",
			wantReason: reasonRequired,
		},
		{
			name: "user id must not be empty",
			changeBody: func(body *createTripRequest) {
				*body.UserID = uuid.Nil
			},
			wantField:  "user_id",
			wantReason: "must be non-empty UUID",
		},
		{
			name: "driver id is required",
			changeBody: func(body *createTripRequest) {
				body.DriverID = nil
			},
			wantField:  "driver_id",
			wantReason: reasonRequired,
		},
		{
			name: "driver id must not be empty",
			changeBody: func(body *createTripRequest) {
				*body.DriverID = uuid.Nil
			},
			wantField:  "driver_id",
			wantReason: "must be non-empty UUID",
		},
		{
			name: "start point is required",
			changeBody: func(body *createTripRequest) {
				body.StartPoint = nil
			},
			wantField:  "start_point",
			wantReason: reasonRequired,
		},
		{
			name: "start latitude is required",
			changeBody: func(body *createTripRequest) {
				body.StartPoint.Latitude = nil
			},
			wantField:  "start_point.latitude",
			wantReason: reasonRequired,
		},
		{
			name: "start latitude is out of range",
			changeBody: func(body *createTripRequest) {
				value := 91.0
				body.StartPoint.Latitude = &value
			},
			wantField:  "start_point.latitude",
			wantReason: "must be between -90 and 90",
		},
		{
			name: "start longitude is required",
			changeBody: func(body *createTripRequest) {
				body.StartPoint.Longitude = nil
			},
			wantField:  "start_point.longitude",
			wantReason: reasonRequired,
		},
		{
			name: "start longitude is out of range",
			changeBody: func(body *createTripRequest) {
				value := 181.0
				body.StartPoint.Longitude = &value
			},
			wantField:  "start_point.longitude",
			wantReason: "must be between -180 and 180",
		},
		{
			name: "end point is required",
			changeBody: func(body *createTripRequest) {
				body.EndPoint = nil
			},
			wantField:  "end_point",
			wantReason: reasonRequired,
		},
		{
			name: "end latitude is out of range",
			changeBody: func(body *createTripRequest) {
				value := -91.0
				body.EndPoint.Latitude = &value
			},
			wantField:  "end_point.latitude",
			wantReason: "must be between -90 and 90",
		},
		{
			name: "end latitude is required",
			changeBody: func(body *createTripRequest) {
				body.EndPoint.Latitude = nil
			},
			wantField:  "end_point.latitude",
			wantReason: reasonRequired,
		},
		{
			name: "end longitude is out of range",
			changeBody: func(body *createTripRequest) {
				value := -181.0
				body.EndPoint.Longitude = &value
			},
			wantField:  "end_point.longitude",
			wantReason: "must be between -180 and 180",
		},
		{
			name: "end longitude is required",
			changeBody: func(body *createTripRequest) {
				body.EndPoint.Longitude = nil
			},
			wantField:  "end_point.longitude",
			wantReason: reasonRequired,
		},
		{
			name: "price is required",
			changeBody: func(body *createTripRequest) {
				body.Price = nil
			},
			wantField:  "price",
			wantReason: reasonRequired,
		},
		{
			name: "price must be non-negative",
			changeBody: func(body *createTripRequest) {
				value := int64(-1)
				body.Price = &value
			},
			wantField:  "price",
			wantReason: "must be non-negative",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			body := validCreateTripRequest()
			if test.changeBody != nil {
				test.changeBody(&body)
			}

			err := validateCreateTripRequest(body)
			if test.wantField == "" {
				if err != nil {
					t.Fatalf("validateCreateTripRequest() error = %v, want nil", err)
				}

				return
			}

			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("validateCreateTripRequest() error = %T, want *ValidationError", err)
			}
			if validationErr.Field != test.wantField {
				t.Errorf("ValidationError.Field = %q, want %q", validationErr.Field, test.wantField)
			}
			if validationErr.Reason != test.wantReason {
				t.Errorf("ValidationError.Reason = %q, want %q", validationErr.Reason, test.wantReason)
			}
			if validationErr.Error() != test.wantField+" "+test.wantReason {
				t.Errorf("ValidationError.Error() = %q, want %q", validationErr.Error(), test.wantField+" "+test.wantReason)
			}
		})
	}
}

func validCreateTripRequest() createTripRequest {
	userID := uuid.New()
	driverID := uuid.New()
	startLatitude := 55.751244
	startLongitude := 37.618423
	endLatitude := 59.934280
	endLongitude := 30.335099
	price := int64(1500)

	return createTripRequest{
		UserID:   &userID,
		DriverID: &driverID,
		StartPoint: &coordinatesRequest{
			Latitude:  &startLatitude,
			Longitude: &startLongitude,
		},
		EndPoint: &coordinatesRequest{
			Latitude:  &endLatitude,
			Longitude: &endLongitude,
		},
		Price: &price,
	}
}
