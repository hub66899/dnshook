package config

type Manager[T any] interface {
	Get() T
	Update(T) error
	Watch(...func(T)) error
}
