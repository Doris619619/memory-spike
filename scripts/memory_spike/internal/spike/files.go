// 文件用途：在仓库外读写实验数据，冻结哈希并拒绝覆盖已有产物。
package spike

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Hash 返回内容摘要；只用于固定输入版本，不替代数据匿名化。
func Hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// SampleHash 按统一 JSON 编码计算样本摘要，绑定人工金标与实际模型输入。
func SampleHash(s Sample) string { b, _ := json.Marshal(s); return Hash(b) }

// PrivatePath 拒绝任意 Git 仓库内的实验产物路径，也检查已有父目录的符号链接。
func PrivatePath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for p := abs; ; p = filepath.Dir(p) {
		if _, err := os.Stat(p); err == nil {
			real, e := filepath.EvalSymlinks(p)
			if e != nil {
				return e
			}
			for q := real; ; q = filepath.Dir(q) {
				if _, e := os.Stat(filepath.Join(q, ".git")); e == nil {
					return fmt.Errorf("实验数据必须在 Git 仓库之外: %s", path)
				}
				if filepath.Dir(q) == q {
					break
				}
			}
			return nil
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return nil
}

// WriteNew 只创建新文件；目录权限尽量收紧，Windows 仍继承父目录 ACL。
func WriteNew(path string, data []byte) error {
	if err := PrivatePath(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// WriteJSON 固定缩进，方便用户检查匿名样本、配置和人工标注文件。
func WriteJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return WriteNew(path, append(b, '\n'))
}

// ReadJSONL 读取有界 JSONL；空行忽略，损坏的行直接报错而不静默跳过。
func ReadJSONL[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 16<<20)
	var values []T
	line := 0
	for scanner.Scan() {
		line++
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var value T
		if err := json.Unmarshal(scanner.Bytes(), &value); err != nil {
			return nil, fmt.Errorf("第 %d 行 JSON 不合法", line)
		}
		values = append(values, value)
	}
	return values, scanner.Err()
}

// AppendJSONL 每条运行状态写入后立即同步磁盘，避免进程崩溃导致同组合再次调用。
func AppendJSONL(f *os.File, value any) error {
	if err := json.NewEncoder(f).Encode(value); err != nil {
		return err
	}
	return f.Sync()
}
