package admin

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// loadYAML 读取 path 处的 YAML 文件并解码到 out。
func loadYAML(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, out); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

// atomicWriteYAML 把 cfg 序列化后通过 tmp + rename 原子写入 path（权限 0600）。
//
// 写入文件含上游凭据，故权限统一收紧至 0600 防止同机其他用户读取。
func atomicWriteYAML(path string, cfg any) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal yaml: %w", err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}
