package agent

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// LoadEnv 从 .env 文件加载环境变量。
// 支持格式：KEY=VALUE、KEY="VALUE"、# 注释、空行。
// 已存在的环境变量不会被覆盖。文件不存在时静默跳过（返回 nil）。
func LoadEnv(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		idx := strings.Index(line, "=")
		if idx == -1 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])

		// 支持引号包裹的值
		if (strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`)) ||
			(strings.HasPrefix(value, `'`) && strings.HasSuffix(value, `'`)) {
			value = value[1 : len(value)-1]
		}

		// 不覆盖已存在的环境变量
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf("设置环境变量 %s 失败: %w", key, err)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if closeErr := f.Close(); closeErr != nil {
			return fmt.Errorf("读取 .env 文件失败: %v; 关闭文件失败: %w", err, closeErr)
		}
		return fmt.Errorf("读取 .env 文件失败: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("关闭 .env 文件失败: %w", err)
	}
	return nil
}
