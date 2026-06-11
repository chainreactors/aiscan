package types

// TypedResult is the SDK-wide transport wrapper used by Engine.Execute.
// The payload type stays explicit, for example *types.GOGOResult.
type TypedResult[T any] struct {
	success bool
	err     error
	data    T
}

// NewResult wraps engine-specific payload data in the common SDK Result shape.
func NewResult[T any](success bool, err error, data T) *TypedResult[T] {
	return &TypedResult[T]{
		success: success,
		err:     err,
		data:    data,
	}
}

func (r *TypedResult[T]) Success() bool {
	return r != nil && r.success
}

func (r *TypedResult[T]) Error() error {
	if r == nil {
		return nil
	}
	return r.err
}

func (r *TypedResult[T]) Data() interface{} {
	if r == nil {
		return nil
	}
	return r.data
}

// Value returns the typed payload without forcing callers through interface{}.
func (r *TypedResult[T]) Value() T {
	var zero T
	if r == nil {
		return zero
	}
	return r.data
}

// ResultData extracts a typed payload from any SDK Result.
func ResultData[T any](result Result) (T, bool) {
	var zero T
	if result == nil {
		return zero, false
	}
	if typed, ok := result.(interface{ Value() T }); ok {
		return typed.Value(), true
	}
	data, ok := result.Data().(T)
	return data, ok
}
