package response

import (
	"encoding/json"
	"log/slog"
	"net/http"

	api "github.com/ogrock3t/go-lw-avito/internal/generated"
)

func (writer *Writer) WriteProblem(w http.ResponseWriter, r *http.Request, statusCode int32, code, title, detail string) {
	instance := r.URL.Path

	writer.WriteProblemBody(w, r, int(statusCode), api.Problem{
		Type:     "https://tripgo.example/problems/" + code,
		Title:    title,
		Status:   statusCode,
		Detail:   &detail,
		Instance: &instance,
		Code:     code,
	})
}

func (writer *Writer) WriteProblemBody(w http.ResponseWriter, r *http.Request, statusCode int, problem api.Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(problem); err != nil {
		writer.logger.ErrorContext(r.Context(), "write problem response", slog.Any("error", err))
	}
}
