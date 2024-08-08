package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/luiz504/week-tech-go-server/internal/helpers"
	"github.com/luiz504/week-tech-go-server/internal/mappers"
	"github.com/luiz504/week-tech-go-server/internal/store/pg"
	"github.com/luiz504/week-tech-go-server/internal/utils"
)

type apiHandler struct {
	q           *pg.Queries
	r           *chi.Mux
	upgrader    websocket.Upgrader
	subscribers map[string]map[*websocket.Conn]context.CancelFunc
	mu          *sync.Mutex
}

func (h apiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.r.ServeHTTP(w, r)
}

func NewHandler(q *pg.Queries) http.Handler {
	a := apiHandler{
		q:           q,
		upgrader:    websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}, // TODO: allow only production
		subscribers: make(map[string]map[*websocket.Conn]context.CancelFunc),
		mu:          &sync.Mutex{},
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer, middleware.Logger)
	r.Use(
		cors.Handler(
			cors.Options{
				AllowedOrigins:   []string{"http://*", "https://*"}, // TODO: allow only production
				AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
				AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
				ExposedHeaders:   []string{"Link"},
				AllowCredentials: false,
				MaxAge:           300,
			},
		),
	)

	r.Get("/subscribe/{room_id}", a.handleSubscribeToRoom)

	r.Route("/api", func(r chi.Router) {
		r.Route("/rooms", func(r chi.Router) {
			r.Post("/", a.handleCreateRoom)
			r.Get("/", a.handleGetRooms)

			r.Route("/{room_id}/messages", func(r chi.Router) {
				r.Post("/", a.handleCreateRoomMessage)
				r.Get("/", a.handleGetRoomMessages)

				r.Route("/{message_id}", func(r chi.Router) {
					r.Get("/", a.handleGetRoomMessage)
					r.Patch("/react", a.handleReactToMessage)
					r.Delete("/react", a.handleRemoveReactionFromMessage)
					r.Patch("/answer", a.handleMarkMessageAsAnswered)
				})

			})
		})
	})

	a.r = r

	return a
}

// * WS Controllers
func (h apiHandler) handleSubscribeToRoom(w http.ResponseWriter, r *http.Request) {

	roomId, err := utils.ParseUUIDParam(r, "room_id")
	if err != nil {
		http.Error(w, "invalid room id", http.StatusBadRequest)
		return
	}

	_, err = h.q.GetRoom(r.Context(), roomId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "room not found", http.StatusNotFound)
			return
		}
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}

	c, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		msg := "failed to upgrade connection"
		helpers.LogErrorAndRespond(w, msg, err, msg, http.StatusBadRequest)
		return
	}

	defer c.Close()

	ctx, cancel := context.WithCancel(r.Context())

	h.mu.Lock()
	if _, ok := h.subscribers[roomId.String()]; !ok {
		h.subscribers[roomId.String()] = make(map[*websocket.Conn]context.CancelFunc)
	}
	h.subscribers[roomId.String()][c] = cancel

	h.mu.Unlock()

	slog.Info("new subscriber connected", "room_id", roomId.String(), "client_ip", r.RemoteAddr)
	<-ctx.Done()
	//? Will be called when the client closes the connection
	h.mu.Lock()

	delete(h.subscribers[roomId.String()], c)

	h.mu.Unlock()
}

const (
	MessageKindMessageCreated = "message_created"
)

type MessageMessageCreated struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}
type Message struct {
	Kind   string `json:"kind"`
	Value  any    `json:"value"`
	RoomID string `json:"-"`
}

func (h apiHandler) notifyClients(msg Message) {
	h.mu.Lock()
	defer h.mu.Unlock()

	subscribers, ok := h.subscribers[msg.RoomID]
	if !ok || len(subscribers) == 0 {
		return
	}

	for conn, cancel := range subscribers {
		if err := conn.WriteJSON(msg); err != nil {
			slog.Error("failed to send message to client", "error", err)
			cancel()
			//* this call will trigger the handleSubscribeToRoom cleanup
		}
	}
}

// * HTTP Controllers
func (h apiHandler) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	type _body struct {
		Theme string `json:"theme"`
	}
	var body _body
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	roomId, err := h.q.InsertRoom(r.Context(), body.Theme)
	if err != nil {
		helpers.LogErrorAndRespond(w, "failed to insert room", err, "something went wrong", http.StatusInternalServerError)
		return
	}

	type response struct {
		ID string `json:"id"`
	}

	data, err := json.Marshal(response{ID: roomId.String()})
	if err != nil {
		helpers.LogErrorAndRespond(w, "failed to marshal response", err, "something went wrong", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, err = w.Write(data)
	if err != nil {
		helpers.LogErrorAndRespond(w, "failed to write response", err, "something went wrong", http.StatusInternalServerError)
		return
	}
}

func (h apiHandler) handleGetRooms(w http.ResponseWriter, r *http.Request) {
	// Todo:  Add Pagination

	rooms, err := h.q.GetRooms(r.Context())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "room not found", http.StatusNotFound)
			return
		}
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}

	type response struct {
		Rooms []pg.Room `json:"rooms"`
	}

	data, err := json.Marshal(response{Rooms: rooms})
	if err != nil {
		helpers.LogErrorAndRespond(w, "failed to marshal response", err, "something went wrong", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(data)
	if err != nil {
		helpers.LogErrorAndRespond(w, "failed to write response", err, "something went wrong", http.StatusInternalServerError)
		return
	}
}

func (h apiHandler) handleCreateRoomMessage(w http.ResponseWriter, r *http.Request) {
	roomId, err := utils.ParseUUIDParam(r, "room_id")
	if err != nil {
		http.Error(w, "invalid room id", http.StatusBadRequest)
		return
	}

	_, err = h.q.GetRoom(r.Context(), roomId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "room not found", http.StatusNotFound)
			return
		}
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}

	type _body struct {
		Message string `json:"message"`
	}
	var body _body
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	messageID, err := h.q.InsertMessage(r.Context(), pg.InsertMessageParams{RoomID: roomId, Message: body.Message})
	if err != nil {
		helpers.LogErrorAndRespond(w, "failed to insert message", err, "something went wrong", http.StatusInternalServerError)
		return
	}

	type response struct {
		ID string `json:"id"`
	}

	data, err := json.Marshal(response{ID: messageID.String()})
	if err != nil {
		helpers.LogErrorAndRespond(w, "failed to marshal response", err, "something went wrong", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, err = w.Write(data)
	if err != nil {
		helpers.LogErrorAndRespond(w, "failed to write response", err, "something went wrong", http.StatusInternalServerError)
		return
	}

	go h.notifyClients(Message{
		Kind:   MessageKindMessageCreated,
		RoomID: roomId.String(),
		Value: MessageMessageCreated{
			ID:      messageID.String(),
			Message: body.Message,
		}})
}

func (h apiHandler) handleGetRoomMessages(w http.ResponseWriter, r *http.Request) {
	roomId, err := utils.ParseUUIDParam(r, "room_id")
	if err != nil {
		http.Error(w, "invalid room id", http.StatusBadRequest)
		return
	}
	messages, err := h.q.GetRoomMessages(r.Context(), roomId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "room not found", http.StatusNotFound)
			return
		}
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}

	type response struct {
		RoomID   string                `json:"room_id"`
		Messages []mappers.RoomMessage `json:"messages"`
	}

	data, err := json.Marshal(response{RoomID: roomId.String(), Messages: mappers.MapMessageToRoomMessage(messages)})
	if err != nil {
		helpers.LogErrorAndRespond(w, "failed to marshal response", err, "something went wrong", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(data)
	if err != nil {
		helpers.LogErrorAndRespond(w, "failed to write response", err, "something went wrong", http.StatusInternalServerError)
		return
	}
}

func (h apiHandler) handleGetRoomMessage(w http.ResponseWriter, r *http.Request) {
	roomID, err := utils.ParseUUIDParam(r, "room_id")
	if err != nil {
		http.Error(w, "invalid room id", http.StatusBadRequest)
		return
	}
	messageId, err := utils.ParseUUIDParam(r, "message_id")
	if err != nil {
		http.Error(w, "invalid message id", http.StatusBadRequest)
		return
	}
	message, err := h.q.GetMessage(r.Context(), messageId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "message not found", http.StatusNotFound)
			return
		}
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}

	if message.RoomID.String() != roomID.String() {
		http.Error(w, "message not found", http.StatusNotFound)
		return
	}

	type response struct {
		Message pg.Message `json:"message"`
	}

	data, err := json.Marshal(response{Message: message})
	if err != nil {
		helpers.LogErrorAndRespond(w, "failed to marshal response", err, "something went wrong", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(data)
	if err != nil {
		helpers.LogErrorAndRespond(w, "failed to write response", err, "something went wrong", http.StatusInternalServerError)
		return
	}
}

func (h apiHandler) handleReactToMessage(w http.ResponseWriter, r *http.Request) {
	roomID, err := utils.ParseUUIDParam(r, "room_id")
	if err != nil {
		http.Error(w, "invalid room id", http.StatusBadRequest)
		return
	}
	messageId, err := utils.ParseUUIDParam(r, "message_id")
	if err != nil {
		http.Error(w, "invalid message id", http.StatusBadRequest)
		return
	}
	message, err := h.q.GetMessage(r.Context(), messageId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "message not found", http.StatusNotFound)
			return
		}
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}

	if message.RoomID.String() != roomID.String() {
		http.Error(w, "message not found", http.StatusNotFound)
		return
	}

	count, err := h.q.ReactToMessage(r.Context(), messageId)
	if err != nil {
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}

	type response struct {
		Count int64 `json:"count"`
	}

	data, err := json.Marshal(response{Count: count})
	if err != nil {
		helpers.LogErrorAndRespond(w, "failed to marshal response", err, "something went wrong", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(data)
	if err != nil {
		helpers.LogErrorAndRespond(w, "failed to write response", err, "something went wrong", http.StatusInternalServerError)
		return
	}

}
func (h apiHandler) handleRemoveReactionFromMessage(w http.ResponseWriter, r *http.Request) {
	roomID, err := utils.ParseUUIDParam(r, "room_id")
	if err != nil {
		http.Error(w, "invalid room id", http.StatusBadRequest)
		return
	}
	messageId, err := utils.ParseUUIDParam(r, "message_id")
	if err != nil {
		http.Error(w, "invalid message id", http.StatusBadRequest)
		return
	}
	message, err := h.q.GetMessage(r.Context(), messageId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "message not found", http.StatusNotFound)
			return
		}
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}

	if message.RoomID.String() != roomID.String() {
		http.Error(w, "message not found", http.StatusNotFound)
		return
	}

	if message.ReactionCount == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	count, err := h.q.RemoveReactionFromMessage(r.Context(), messageId)
	if err != nil {
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}

	type response struct {
		Count int64 `json:"count"`
	}

	data, err := json.Marshal(response{Count: count})
	if err != nil {
		helpers.LogErrorAndRespond(w, "failed to marshal response", err, "something went wrong", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(data)
	if err != nil {
		helpers.LogErrorAndRespond(w, "failed to write response", err, "something went wrong", http.StatusInternalServerError)
		return
	}
}

func (h apiHandler) handleMarkMessageAsAnswered(w http.ResponseWriter, r *http.Request) {
	roomID, err := utils.ParseUUIDParam(r, "room_id")
	if err != nil {
		http.Error(w, "invalid room id", http.StatusBadRequest)
		return
	}
	messageId, err := utils.ParseUUIDParam(r, "message_id")
	if err != nil {
		http.Error(w, "invalid message id", http.StatusBadRequest)
		return
	}
	message, err := h.q.GetMessage(r.Context(), messageId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "message not found", http.StatusNotFound)
			return
		}
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}

	if message.RoomID.String() != roomID.String() {
		http.Error(w, "message not found", http.StatusNotFound)
		return
	}
	if message.Answered {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	err = h.q.MarkMessageAsAnswered(r.Context(), messageId)
	if err != nil {
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
