package helpers

func DefaultIfNil[T any](nillablePtr *T, defaultValue T) T {
	if nillablePtr == nil {
		return defaultValue
	}
	return *nillablePtr
}

func DefaultIfEmpty(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}
