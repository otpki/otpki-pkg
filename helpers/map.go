package helpers

func ContainsValue[K comparable, V comparable](m map[K]V, targetValue V) bool {
	for _, value := range m {
		if value == targetValue {
			return true
		}
	}
	return false
}

func ContainsKey[K comparable, V comparable](m map[K]V, targetKey K) bool {
	for key := range m {
		if key == targetKey {
			return true
		}
	}
	return false
}

func FlipMap[K comparable, V comparable](inputMap map[K]V) map[V]K {
	flippedMap := make(map[V]K)
	for key, value := range inputMap {
		flippedMap[value] = key
	}
	return flippedMap
}
