package helpers

import (
	"log/slog"
	"net/http"
)

func LogErrorAndRespond(w http.ResponseWriter, logMessage string, err error, responseMessage string, code int) {
	slog.Warn(logMessage, "error", err)
	http.Error(w, responseMessage, code)
}
