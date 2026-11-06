package response

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type Writer struct {
	logger *slog.Logger
}

func NewWriter(logger *slog.Logger) *Writer {
	return &Writer{logger: logger}
}

func (writer *Writer) WriteJSON(w http.ResponseWriter, r *http.Request, statusCode int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(body); err != nil {
		writer.logger.ErrorContext(r.Context(), "write json response", slog.Any("error", err))
	}
}
