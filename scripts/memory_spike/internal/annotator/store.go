// 文件用途：保存真人逐题判断，以单一原子文件绑定进度、金标和冻结样本。
package annotator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"memory-spike/internal/spike"
)

type Mark struct {
	Reviewed bool         `json:"reviewed,omitempty"`
	Status   string       `json:"status"`
	Entries  []spike.Gold `json:"entries"`
}
type State struct {
	Description string           `json:"description"`
	Revision    int              `json:"revision"`
	Current     int              `json:"current"`
	SavedAt     string           `json:"saved_at"`
	Marks       map[string]Mark  `json:"marks"`
	Annotation  spike.Annotation `json:"annotation"`
}
type Store struct {
	Suggestions *Suggestions
	mu          sync.Mutex
	Preview     spike.CodexPreview
	Path        string
	state       State
}

// Open 检查冻结输入与磁盘进度；损坏或换样本时拒绝覆盖，不预填任何人工判断。
func Open(previewPath, statePath string) (*Store, error) {
	if err := spike.PrivatePath(statePath); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(previewPath)
	if err != nil {
		return nil, err
	}
	s := &Store{Path: statePath}
	if err = json.Unmarshal(b, &s.Preview); err != nil {
		return nil, err
	}
	p := s.Preview
	if !p.LocalProcessingConsent || p.SampleHash != spike.SampleHash(p.Sample) || len(p.Sample.Messages) == 0 {
		return nil, fmt.Errorf("样本未获本地处理同意、为空或摘要不匹配")
	}
	ids := map[string]bool{}
	for _, m := range p.Sample.Messages {
		if m.ID == "" || ids[m.ID] {
			return nil, fmt.Errorf("样本消息 ID 缺失或重复")
		}
		ids[m.ID] = true
	}
	s.state = State{Description: "真人本地标注进度；annotation 是同一版本的金标，complete=false 时不可用于评估。", Marks: map[string]Mark{}, Annotation: spike.Annotation{SampleID: p.Sample.ID, SampleHash: p.SampleHash, Annotator: "participant-1", Entries: []spike.Gold{}}}
	if err = s.loadSuggestions(filepath.Join(filepath.Dir(previewPath), "ai-suggestions.json")); err != nil {
		return nil, err
	}
	b, err = os.ReadFile(statePath)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &s.state); err != nil {
		return nil, err
	}
	if s.state.Annotation.SampleHash != p.SampleHash || s.state.Annotation.SampleID != p.Sample.ID {
		return nil, fmt.Errorf("已有进度属于不同样本，请使用另一输出路径")
	}
	if err = s.validate(&s.state); err != nil {
		return nil, fmt.Errorf("已有进度无效: %w", err)
	}
	return s, nil
}

// Snapshot 返回深拷贝，避免 HTTP 编码与下一次保存共享可变 map。
func (s *Store) Snapshot() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(s.state)
	var state State
	_ = json.Unmarshal(b, &state)
	return state
}

// validate 允许未填完的草稿；完成提交必须逐题明确判断并填写有效事实及证据。
func (s *Store) validate(next *State) error {
	if s.Suggestions != nil || s.state.Annotation.Method == "ai_assisted" {
		next.Annotation.Method = "ai_assisted"
	}
	if next.Current < 0 || next.Current >= len(s.Preview.Sample.Messages) {
		return fmt.Errorf("题号无效")
	}
	if strings.TrimSpace(next.Annotation.Annotator) == "" {
		return fmt.Errorf("标注者代号不能为空")
	}
	known := map[string]bool{}
	for _, m := range s.Preview.Sample.Messages {
		known[m.ID] = true
	}
	for id := range next.Marks {
		if !known[id] {
			return fmt.Errorf("存在不属于样本的题目")
		}
	}
	entries := []spike.Gold{}
	for _, m := range s.Preview.Sample.Messages {
		mark := next.Marks[m.ID]
		if next.Annotation.Complete && s.Suggestions != nil && !mark.Reviewed {
			return fmt.Errorf("还有未经本人确认的 AI 建议，请逐条确认")
		}
		if mark.Status != "" && mark.Status != "skip" && mark.Status != "uncertain" && mark.Status != "marked" {
			return fmt.Errorf("题目状态无效")
		}
		if next.Annotation.Complete && (mark.Status == "" || mark.Status == "uncertain") {
			return fmt.Errorf("还有未完成或拿不准的题目，请先逐条确认")
		}
		if mark.Status != "marked" && len(mark.Entries) > 0 {
			return fmt.Errorf("非标记状态不能包含事实")
		}
		if next.Annotation.Complete && mark.Status == "marked" && len(mark.Entries) == 0 {
			return fmt.Errorf("标记题目需要至少一条事实")
		}
		for _, g := range mark.Entries {
			if g.Category != "auto" && g.Category != "temporary" && g.Category != "sensitive" {
				return fmt.Errorf("事实类别无效")
			}
			if len(g.Evidence) == 0 || g.Evidence[0] != m.ID {
				return fmt.Errorf("事实必须引用当前原话")
			}
			for _, id := range g.Evidence {
				if !known[id] {
					return fmt.Errorf("证据不属于样本")
				}
			}
			if g.Stability != "" && g.Stability != "用户明确自述" && g.Stability != "多次出现的行为信号" && g.Stability != "单次弱信号" {
				return fmt.Errorf("依据类型无效")
			}
			g.ID = fmt.Sprintf("g%d", len(entries)+1)
			entries = append(entries, g)
		}
	}
	next.Annotation.SampleID = s.Preview.Sample.ID
	next.Annotation.SampleHash = s.Preview.SampleHash
	next.Annotation.Entries = entries
	if next.Annotation.Complete {
		if errors := spike.ValidateAnnotations(s.Preview.Sample, []spike.Annotation{next.Annotation}); len(errors) > 0 {
			return fmt.Errorf("%s", strings.Join(errors, "；"))
		}
		// 合成验收样本也要遵守同样的人工字段要求。
		for _, g := range entries {
			if strings.TrimSpace(g.Fact) == "" || g.Stability == "" {
				return fmt.Errorf("请补齐每条事实和判断依据")
			}
		}
	}
	return nil
}

// Save 使用版本比较阻止多标签页覆盖；进度与金标一次原子替换，重新编辑会撤销完成状态。
func (s *Store) Save(next State) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if next.Revision != s.state.Revision {
		return State{}, fmt.Errorf("进度已在另一个页面更新，请先保存当前文字到别处，再刷新此页")
	}
	if err := s.validate(&next); err != nil {
		return State{}, err
	}
	next.Description = s.state.Description
	next.Revision++
	next.SavedAt = time.Now().UTC().Format(time.RFC3339)
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return State{}, err
	}
	if err = atomicWrite(s.Path, append(b, '\n')); err != nil {
		return State{}, fmt.Errorf("写入本机失败，当前页面仍保留你的选择: %w", err)
	}
	s.state = next
	return next, nil
}

// atomicWrite 在目标目录创建临时文件并同步后替换，任何失败都保留旧进度。
func atomicWrite(path string, b []byte) error {
	if err := spike.PrivatePath(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".annotation-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
