package config

import (
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
)

// LocalYamlConfig 功能：获取配置管理器
func LocalYamlConfig[T any](fileName string, initialConfig ...T) (Manager[T], error) {
	m := localImpl[T]{
		fileName: fileName,
	}
	if len(initialConfig) > 0 {
		d := initialConfig[0]
		m.data = &d
	}
	exist, err := m.load()
	if err != nil {
		return nil, err
	}
	if !exist {
		if err = m.write(*m.data); err != nil {
			return nil, err
		}
	}
	return &m, nil
}

type localImpl[T any] struct {
	fileName string
	data     *T
}

func (m *localImpl[T]) GetConfig() *T {
	return m.data
}

func (m *localImpl[T]) UpdateConfig(config T) error {
	if err := m.write(config); err != nil {
		return err
	}
	m.data = &config
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
	// yaml转json
	err = yaml.Unmarshal(yamlFile, m.data)
	return true, errors.WithStack(err)
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
