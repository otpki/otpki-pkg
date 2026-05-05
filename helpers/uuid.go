package helpers

import "github.com/google/uuid"

// Byte methods

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

// String methods

func NullUUIDToString(u uuid.NullUUID) *string {
	if !u.Valid {
		return nil
	}
	return new(u.UUID.String())
}

func StringToNullUUID(s *string) uuid.NullUUID {
	if s == nil {
		return uuid.NullUUID{}
	}
	parsed, err := uuid.Parse(*s)
	if err != nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: parsed, Valid: true}
}

func UUIDSliceToStringSlice(uuids []uuid.UUID) []string {
	out := make([]string, len(uuids))
	for i, u := range uuids {
		out[i] = u.String()
	}
	return out
}

func NullUUIDSliceToStringSlice(uuids []uuid.NullUUID) []*string {
	out := make([]*string, len(uuids))
	for i, u := range uuids {
		out[i] = NullUUIDToString(u)
	}
	return out
}
