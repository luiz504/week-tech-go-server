package mappers

import "github.com/luiz504/week-tech-go-server/internal/store/pg"

type RoomMessage struct {
	ID            string `json:"id"`
	RoomID        string `json:"room_id"`
	Message       string `json:"message"`
	ReactionCount int64  `json:"reaction_count"`
	Answered      bool   `json:"answered"`
}

func MapMessageToRoomMessage(messages []pg.Message) []RoomMessage {
	var roomMessages []RoomMessage
	for _, message := range messages {
		roomMessages = append(roomMessages, RoomMessage{
			ID:            message.ID.String(),
			RoomID:        message.RoomID.String(),
			Message:       message.Message,
			ReactionCount: message.ReactionCount,
			Answered:      message.Answered,
		})
	}
	return roomMessages
}
