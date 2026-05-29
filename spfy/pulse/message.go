package pulse

import (
	"bytes"
	"time"
)

type Message struct {
	Header Header
	Data   []byte
}

func (m Message) Clone() Message {
	return Message{
		Header: m.Header.Clone(),
		Data:   bytes.Clone(m.Data),
	}
}

type Envelope struct {
	ID            string
	Subject       string
	Message       Message
	Timestamp     time.Time
	CorrelationID string
	ReplyTo       string
}

func (e Envelope) Clone() Envelope {
	return Envelope{
		ID:            e.ID,
		Subject:       e.Subject,
		Message:       e.Message.Clone(),
		Timestamp:     e.Timestamp,
		CorrelationID: e.CorrelationID,
		ReplyTo:       e.ReplyTo,
	}
}
