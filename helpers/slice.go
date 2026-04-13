package helpers

func RemoveAtIndex[T any](arr []T, index int) []T {
	// take the item from the end and move it into this spot
	arr[index] = arr[len(arr)-1]
	// remove the item from the end
	return arr[:len(arr)-1]
}

func RemoveAtIndexOrdered[T any](arr []T, index int) []T {
	return append(arr[:index], arr[index+1:]...)
}
