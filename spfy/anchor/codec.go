package anchor

import "encoding/json"

type Codec[T any] interface {
	Marshal(T) ([]byte, error)
	Unmarshal([]byte) (T, error)
}

type JSONCodec[T any] struct{}

func (JSONCodec[T]) Marshal(v T) ([]byte, error) { return json.Marshal(v) }

func (JSONCodec[T]) Unmarshal(b []byte) (T, error) {
	var v T
	err := json.Unmarshal(b, &v)
	return v, err
}
