package config

import (
	"context"
	"dnshook/pkg/shutdown"
	"github.com/fsnotify/fsnotify"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
)

// LocalYamlConfig 功能：获取配置管理器
func LocalYamlConfig[T any](fileName string, initialConfig ...T) Manager[T] {
	m := localImpl[T]{
		fileName: fileName,
	}
	exist, err := m.load()
	if err != nil {
		logrus.WithError(err).Fatal()
	}
	if !exist {
		if len(initialConfig) > 0 {
			d := initialConfig[0]
			m.data = d
			if err = m.write(d); err != nil {
				logrus.WithError(err).Fatal()
			}
		}
	}
	return &m
}

type localImpl[T any] struct {
	fileName string
	data     T
	listener []func(T)
	watcher  *fsnotify.Watcher
}

func (m *localImpl[T]) Get() T {
	return m.data
}

func (m *localImpl[T]) Update(config T) error {
	if err := m.write(config); err != nil {
		return err
	}
	m.data = config
	return nil
}

func (m *localImpl[T]) Watch(f ...func(T)) error {
	if err := m.startWatch(); err != nil {
		return err
	}
	m.listener = append(m.listener, f...)
	return nil
}

func (m *localImpl[T]) startWatch() error {
	if m.watcher != nil {
		return nil
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return errors.WithStack(err)
	}
	m.watcher = w
	shutdown.OnShutdown(func(ctx context.Context) error {
		return w.Close()
	})
	err = w.Add(m.fileName)
	if err != nil {
		return errors.WithStack(err)
	}
	go func() {
		for {
			select {
			case _, ok := <-w.Events:
				if !ok {
					return
				}
				if _, err = m.load(); err != nil {
					logrus.WithError(err).Error()
				}
				for _, f := range m.listener {
					f(m.data)
				}
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				logrus.WithError(err).Error()
			}
		}
	}()
	return nil
}

// load 功能：读取yaml配置文件，将数据解析到 m.data
func (m *localImpl[T]) load() (bool, error) {
	// 读取 YAML 文件
	yamlFile, err := os.ReadFile(m.fileName)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, errors.WithStack(err)
	}
	// yaml反序列化
	var data T
	if err = yaml.Unmarshal(yamlFile, &data); err != nil {
		return true, errors.WithStack(err)
	}
	m.data = data
	return true, nil
}

func (m *localImpl[T]) write(config T) error {
	dir := filepath.Dir(m.fileName)
	// 检查目录是否存在
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			// 目录不存在，创建目录
			if err = os.MkdirAll(dir, 0755); err != nil {
				return errors.WithStack(err)
			}
		}
		return errors.WithStack(err)
	}
	file, err := os.Create(m.fileName)
	if err != nil {
		return err
	}
	defer file.Close()
	out, err := yaml.Marshal(config)
	if err != nil {
		return errors.WithStack(err)
	}
	_, err = file.Write(out)
	return errors.WithStack(err)
}
