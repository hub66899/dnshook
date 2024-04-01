package config

type Manager[T any] interface {
	GetConfig() *T
	UpdateConfig(T) error
}
