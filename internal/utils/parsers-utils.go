package utils

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func ParseUUIDParam(r *http.Request, param string) (uuid.UUID, error) {
	rawRoomID := chi.URLParam(r, param)

	return uuid.Parse(rawRoomID)
}
