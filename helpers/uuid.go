package helpers

import "github.com/google/uuid"

func NullUUIDToByteSlice(uuid uuid.NullUUID) []byte {
	if uuid.Valid {
		return uuid.UUID[:]
	}
	return []byte(nil)
}

func UUIDToByteSlice(uuid uuid.UUID) []byte {
	return uuid[:]
}

func UUIDSliceToByteSlice(uuids []uuid.UUID) [][]byte {
	out := make([][]byte, len(uuids))
	for i, uuid := range uuids {
		out[i] = UUIDToByteSlice(uuid)
	}
	return out
}

func NullUUIDSliceToByteSlice(uuids []uuid.NullUUID) [][]byte {
	out := make([][]byte, len(uuids))
	for i, uuid := range uuids {
		out[i] = NullUUIDToByteSlice(uuid)
	}
	return out
}
