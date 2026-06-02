package pulse

import "encoding/json"

const ServiceName = "pulse"

type EnvelopeError struct {
	Oopsie json.RawMessage `json:"oopsie,omitempty"`
}
