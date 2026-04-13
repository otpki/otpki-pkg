package config

// Validator validates a configuration object after it's loaded.
// Use this for business logic validation ("a + b must be less than c").
// For required fields, use DefaultRegistry.RegisterRequired() instead.
// For per-field validation, use DefaultEntry.Validator instead.
type Validator[T any] interface {
	Validate(config *T) error
}

type ValidatorFunc[T any] func(*T) error

func (f ValidatorFunc[T]) Validate(config *T) error {
	return f(config)
}

func NewValidator[T any](fn func(*T) error) Validator[T] {
	return ValidatorFunc[T](fn)
}
