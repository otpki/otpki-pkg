package helpers

import "github.com/google/uuid"

func NullUUIDToByteSlice(u uuid.NullUUID) []byte {
	if u.Valid {
		return u.UUID[:]
	}
	return []byte(nil)
}

func UUIDToByteSlice(u uuid.UUID) []byte {
	return u[:]
}

func UUIDSliceToByteSlice(uuids []uuid.UUID) [][]byte {
	out := make([][]byte, len(uuids))
	for i, u := range uuids {
		out[i] = UUIDToByteSlice(u)
	}
	return out
}

func NullUUIDSliceToByteSlice(uuids []uuid.NullUUID) [][]byte {
	out := make([][]byte, len(uuids))
	for i, u := range uuids {
		out[i] = NullUUIDToByteSlice(u)
	}
	return out
}
