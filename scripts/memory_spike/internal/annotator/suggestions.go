// 文件用途：加载明确标注来源的 AI 预标注，只作为待真人逐条审阅的建议，不自动填入完成记录。
package annotator

import (
	"encoding/json"
	"fmt"
	"os"
)

type Suggestion struct {
	Mark        Mark   `json:"mark"`
	Reason      string `json:"reason"`
	SourceBasis string `json:"source_basis"`
}
type Suggestions struct {
	Description string                `json:"description"`
	SampleHash  string                `json:"sample_sha256"`
	Author      string                `json:"author"`
	Items       map[string]Suggestion `json:"items"`
}

// loadSuggestions 绑定当前样本并校验完整建议；不存在时保留原独立标注模式。
func (s *Store) loadSuggestions(path string) error {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var suggestions Suggestions
	if err = json.Unmarshal(b, &suggestions); err != nil {
		return err
	}
	if suggestions.SampleHash != s.Preview.SampleHash || suggestions.Author == "" || len(suggestions.Items) != len(s.Preview.Sample.Messages) {
		return fmt.Errorf("AI 建议缺少来源、题目或不对应当前样本")
	}
	draft := s.state
	draft.Marks = map[string]Mark{}
	for _, m := range s.Preview.Sample.Messages {
		proposal, ok := suggestions.Items[m.ID]
		if !ok || proposal.Reason == "" || proposal.Mark.Reviewed {
			return fmt.Errorf("AI 建议必须有理由且保持未审阅")
		}
		draft.Marks[m.ID] = proposal.Mark
	}
	if err = s.validate(&draft); err != nil {
		return fmt.Errorf("AI 建议无效: %w", err)
	}
	s.Suggestions = &suggestions
	s.state.Annotation.Method = "ai_assisted"
	s.state.Description = "AI 预标注后由本人逐条审阅；不是独立人工金标，不能用于正式实验质量结论。"
	return nil
}
