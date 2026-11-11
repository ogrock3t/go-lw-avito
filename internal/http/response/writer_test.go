//go:generate go tool mockgen -destination=mocks_test.go -package=response_test net/http ResponseWriter
package response_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/mock/gomock"

	api "github.com/ogrock3t/go-lw-avito/internal/generated"
	"github.com/ogrock3t/go-lw-avito/internal/http/response"
)

func TestWriter_LogsWriteFailure(t *testing.T) {
	for _, problem := range []bool{false, true} {
		name := "json"
		if problem {
			name = "problem"
		}
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			writer := response.NewWriter(slog.New(slog.NewJSONHandler(&logs, nil)))
			writeErr := errors.New("client disconnected")
			w := NewMockResponseWriter(gomock.NewController(t))
			status := http.StatusOK
			if problem {
				status = http.StatusInternalServerError
			}
			gomock.InOrder(
				w.EXPECT().Header().Return(make(http.Header)),
				w.EXPECT().WriteHeader(status),
				w.EXPECT().Write(gomock.Any()).Return(0, writeErr),
			)
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if problem {
				writer.WriteProblemBody(w, r, status, api.Problem{Code: "internal_error"})
			} else {
				writer.WriteJSON(w, r, status, map[string]string{"status": "ok"})
			}
			var record map[string]any
			if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
				t.Fatalf("decode error log: %v", err)
			}
			if record["error"] != writeErr.Error() || record["level"] != slog.LevelError.String() {
				t.Errorf("write failure log = %v", record)
			}
		})
	}
}
