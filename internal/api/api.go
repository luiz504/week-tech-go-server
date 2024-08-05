package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/luiz504/week-tech-go-server/internal/store/pg"
)

type apiHandler struct {
	q *pg.Queries
	r *chi.Mux
}

func (h apiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.r.ServeHTTP(w, r)
}

func NewHandler(q *pg.Queries) http.Handler {
	a := apiHandler{
		q: q,
	}
	r := chi.NewRouter()

	a.r = r

	return a
}
