package config

import (
	"context"
	"github.com/pkg/errors"
	clientv3 "go.etcd.io/etcd/client/v3"
	"gopkg.in/yaml.v3"
	"time"
)

func EtcdYamlConfig[T any](cli *clientv3.Client, key string, initialConfig ...T) (Manager[T], error) {
	m := etcdImpl[T]{
		cli: cli,
		key: key,
	}
	if len(initialConfig) > 0 {
		d := initialConfig[0]
		m.data = &d
	}
	err := m.load()
	if err != nil {
		if m.data != nil {
			if err = m.set(*m.data); err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}
	return &m, nil
}

type etcdImpl[T any] struct {
	cli  *clientv3.Client
	key  string
	data *T
}

func (m *etcdImpl[T]) GetConfig() *T {
	return m.data
}

func (m *etcdImpl[T]) UpdateConfig(config T) error {
	if err := m.set(config); err != nil {
		return err
	}
	m.data = &config
	return nil
}

func (m *etcdImpl[T]) load() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	res, err := m.cli.Get(ctx, m.key)
	if err != nil {
		return errors.WithStack(err)
	}
	if len(res.Kvs) == 0 {
		return errors.New("key is not defined")
	}
	if len(res.Kvs) != 1 {
		return errors.New("wrong key")
	}
	if res.Kvs[0] == nil {
		return errors.New("data is not exist")
	}
	data := res.Kvs[0].Value
	err = yaml.Unmarshal(data, m.data)
	return errors.WithStack(err)
}

func (m *etcdImpl[T]) set(config T) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	out, err := yaml.Marshal(config)
	if err != nil {
		return errors.WithStack(err)
	}
	_, err = m.cli.Put(ctx, m.key, string(out))
	return errors.WithStack(err)
}
