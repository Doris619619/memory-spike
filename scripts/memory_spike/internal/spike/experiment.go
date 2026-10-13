// 文件用途：冻结实验输入、检查正式矩阵前置条件，并用持久日志避免崩溃后重复调用。
package spike

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LoadInputs 读取文件及目录中的金标，不读取凭据，也不发起网络请求。
func LoadInputs(samplesPath, configPath, goldDir string) ([]Sample, Config, []Annotation, error) {
	samples, err := ReadJSONL[Sample](samplesPath)
	if err != nil {
		return nil, Config{}, nil, err
	}
	var config Config
	b, err := os.ReadFile(configPath)
	if err != nil {
		return nil, config, nil, err
	}
	if err = json.Unmarshal(b, &config); err != nil {
		return nil, config, nil, err
	}
	var annotations []Annotation
	paths, err := filepath.Glob(filepath.Join(goldDir, "*.json"))
	if err != nil {
		return nil, config, nil, err
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, config, nil, err
		}
		var a Annotation
		if err = json.Unmarshal(b, &a); err != nil {
			return nil, config, nil, fmt.Errorf("金标 JSON 解析失败: %s", filepath.Base(p))
		}
		annotations = append(annotations, a)
	}
	return samples, config, annotations, nil
}

// Readiness 将所有缺失项一次列出，未齐全时禁止创建正式模型请求。
func Readiness(samples []Sample, c Config, gold []Annotation) []string {
	problems := ValidateConfig(c)
	for _, a := range gold {
		if a.Method != "" && a.Method != "independent_human" {
			problems = append(problems, a.SampleID+": AI 辅助审阅不能代替正式实验的独立人工金标")
		}
	}
	seen := map[string]bool{}
	for _, s := range samples {
		if seen[s.ID] {
			problems = append(problems, s.ID+": 样本重复")
		}
		seen[s.ID] = true
		for _, p := range ValidateSample(s) {
			problems = append(problems, s.ID+": "+p)
		}
		for _, p := range ValidateAnnotations(s, gold) {
			problems = append(problems, s.ID+": "+p)
		}
	}
	for _, id := range []string{"U1", "U2", "U3", "U4", "U5", "U6"} {
		if !seen[id] {
			problems = append(problems, "缺少 "+id)
		}
	}
	return problems
}

// Execute 为一次实验固定输入和参数；started 写入即占用组合，结果未知时也不自动重发。
func Execute(samples []Sample, c Config, gold []Annotation, prompt, dir string) error {
	if problems := Readiness(samples, c, gold); len(problems) > 0 {
		return fmt.Errorf("实验尚未就绪：\n- %s", strings.Join(problems, "\n- "))
	}
	if err := PrivatePath(dir); err != nil {
		return err
	}
	for _, m := range c.Models {
		if os.Getenv(m.APIKeyEnv) == "" {
			return fmt.Errorf("缺少环境变量 %s", m.APIKeyEnv)
		}
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, ".running.lock"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("实验目录被占用；若进程崩溃，先核对日志再人工移除锁")
	}
	lock.Close()
	defer os.Remove(filepath.Join(dir, ".running.lock"))
	manifest := struct {
		Samples []Sample     `json:"samples"`
		Config  Config       `json:"config"`
		Gold    []Annotation `json:"gold"`
		Prompt  string       `json:"prompt"`
	}{samples, c, gold, prompt}
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")
	manifestPath := filepath.Join(dir, "manifest.json")
	if old, err := os.ReadFile(manifestPath); err == nil {
		if Hash(old) != Hash(append(manifestBytes, '\n')) {
			return fmt.Errorf("输入/金标/参数已变化，不能覆盖既有实验；明确建立新实验目录")
		}
	} else if os.IsNotExist(err) {
		if err := WriteNew(manifestPath, append(manifestBytes, '\n')); err != nil {
			return err
		}
	} else {
		return err
	}
	configBytes, _ := json.Marshal(c)
	configHash := Hash(configBytes)
	logPath := filepath.Join(dir, "runs.jsonl")
	seen := map[string]bool{}
	if _, err := os.Stat(logPath); err == nil {
		prior, err := ReadJSONL[Run](logPath)
		if err != nil {
			return fmt.Errorf("运行日志损坏，禁止自动重发；需人工恢复")
		}
		for _, r := range prior {
			seen[r.ID] = true
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	client := NewHTTPClient(c.TimeoutSeconds)
	for _, s := range samples {
		for _, m := range c.Models {
			for _, v := range c.Variants {
				id := Hash([]byte(s.ID + "|" + m.ID + "|" + v + "|" + Hash(manifestBytes)))[:24]
				if seen[id] {
					fmt.Printf("跳过已占用组合 %s / %s / %s（含失败或执行状态未知）\n", s.ID, m.ID, v)
					continue
				}
				started := Run{ID: id, SampleID: s.ID, SampleHash: SampleHash(s), Provenance: s.Provenance, Model: m, Variant: v, PromptVersion: c.PromptVersion, PromptHash: Hash([]byte(prompt)), ConfigHash: configHash, MaxOutputTokens: c.MaxOutputTokens, StartedAt: time.Now().UTC().Format(time.RFC3339Nano), State: "started"}
				if err := AppendJSONL(f, started); err != nil {
					return err
				}
				seen[id] = true
				result := Call(client, c, m, v, prompt, os.Getenv(m.APIKeyEnv), s)
				result.ID = id
				result.ConfigHash = configHash
				if err := AppendJSONL(f, result); err != nil {
					return err
				}
				fmt.Printf("%s / %s / %s: %s，格式=%s，耗时=%dms\n", s.ID, m.ID, v, result.State, result.FormatFailure, result.LatencyMS)
			}
		}
	}
	return nil
}

// PrepareGold 为每个真实样本生成空标注表；U2/U6 分开两份，绝不预填 AI 参考答案。
func PrepareGold(samples []Sample, dir string) error {
	for _, s := range samples {
		if s.Provenance != "real" {
			continue
		}
		names := []string{"A"}
		if s.ID == "U2" || s.ID == "U6" {
			names = append(names, "B")
		}
		for _, name := range names {
			a := Annotation{SampleID: s.ID, SampleHash: SampleHash(s), Annotator: "", Complete: false, Entries: []Gold{}}
			if err := WriteJSON(filepath.Join(dir, s.ID+"-"+name+".json"), a); err != nil {
				return err
			}
		}
	}
	return nil
}
