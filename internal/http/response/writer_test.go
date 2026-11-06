package response_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	api "github.com/ogrock3t/go-lw-avito/internal/generated"
	"github.com/ogrock3t/go-lw-avito/internal/http/response"
)

type failedWriter struct {
	*httptest.ResponseRecorder
	err error
}

func (w *failedWriter) Write([]byte) (int, error) {
	return 0, w.err
}

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
			w := &failedWriter{ResponseRecorder: httptest.NewRecorder(), err: writeErr}
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if problem {
				writer.WriteProblemBody(w, r, http.StatusInternalServerError, api.Problem{Code: "internal_error"})
			} else {
				writer.WriteJSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
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
