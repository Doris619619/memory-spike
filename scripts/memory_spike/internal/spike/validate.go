// 文件用途：校验样本矩阵、人工金标、模型配置以及模型的 JSON 输出；不代替人工事实评分。
package spike

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"
)

// ValidateSample 校验匿名输入、时间窗、渠道取样计数与文档上限，拒绝用合成 U1/U2/U3/U6 代替真实样本。
func ValidateSample(s Sample) []string {
	var problems []string
	if s.ID != "U1" && s.ID != "U2" && s.ID != "U3" && s.ID != "U4" && s.ID != "U5" && s.ID != "U6" {
		problems = append(problems, "未知样本编号")
	}
	if s.Provenance != "real" && s.Provenance != "synthetic" {
		problems = append(problems, "缺少样本来源")
	}
	if s.ID != "U4" && s.ID != "U5" && s.Provenance != "real" {
		problems = append(problems, "该样本必须来自真实用户")
	}
	if s.Provenance == "real" && !s.Consented {
		problems = append(problems, "缺少知情同意确认")
	}
	start, e1 := time.Parse(time.RFC3339, s.WindowStart)
	end, e2 := time.Parse(time.RFC3339, s.WindowEnd)
	if e1 != nil || e2 != nil || !end.AddDate(0, 0, -30).Equal(start) {
		problems = append(problems, "必须固定连续 30 天时间窗")
	}
	if s.MessageCount != len(s.Messages) || len(s.Messages) > 500 || s.SessionCount > 50 {
		problems = append(problems, "消息计数或取样上限不符")
	}
	seen := map[string]bool{}
	sessions := map[string]bool{}
	tokens := 0.0
	var last time.Time
	for _, m := range s.Messages {
		if m.ID == "" || seen[m.ID] {
			problems = append(problems, "消息 ID 缺失或重复")
		}
		seen[m.ID] = true
		if !strings.HasPrefix(m.ID, "m_") || !strings.HasPrefix(m.Session, "s_") {
			problems = append(problems, "消息或会话 ID 未匿名化")
		}
		sessions[m.Session] = true
		tokens += ApproxTokens(m.Text)
		at, err := time.Parse(time.RFC3339, m.Date)
		if err != nil || at.Before(start) || at.After(end) || at.Before(last) {
			problems = append(problems, "消息日期不合法、超窗或未按时间排序")
		}
		last = at
		if len([]rune(m.Text)) > 2000 || strings.TrimSpace(m.Text) == "" || strings.HasPrefix(strings.TrimSpace(m.Text), "<agent-message") {
			problems = append(problems, "消息未按方案清洗")
		}
	}
	if len(sessions) != s.SessionCount || tokens > 24000 || math.Abs(tokens-s.ApproxTokens) > 0.01 {
		problems = append(problems, "会话计数或近似 token 统计不符")
	}
	if s.EligibleSessions < s.SessionCount || s.EligibleMessages < s.MessageCount {
		problems = append(problems, "取样前计数缺失")
	}
	switch s.ID {
	case "U1":
		if s.EligibleSessions >= 10 || s.EligibleMessages >= 100 {
			problems = append(problems, "U1 应为 <10 会话、<100 消息")
		}
	case "U2":
		if s.EligibleSessions < 20 || s.EligibleSessions > 30 {
			problems = append(problems, "U2 应为 20–30 会话")
		}
	case "U3":
		if s.EligibleSessions <= 50 || s.EligibleMessages <= 500 {
			problems = append(problems, "U3 必须覆盖 >50 会话和 >500 消息的重度输入")
		}
	}
	return problems
}

// ValidateAnnotations 要求模型看到的同一取样输入已被人工标注，U2/U6 必须有不同标注者。
func ValidateAnnotations(s Sample, annotations []Annotation) []string {
	if s.Provenance != "real" {
		return nil
	}
	var problems []string
	annotators := map[string]bool{}
	messages := map[string]bool{}
	for _, m := range s.Messages {
		messages[m.ID] = true
	}
	for _, a := range annotations {
		if a.SampleID != s.ID {
			continue
		}
		if !a.Complete || a.Annotator == "" || a.SampleHash != SampleHash(s) {
			problems = append(problems, "金标未完成或不对应当前样本")
			continue
		}
		if annotators[a.Annotator] {
			problems = append(problems, "同一标注者重复提交")
		}
		annotators[a.Annotator] = true
		ids := map[string]bool{}
		for _, g := range a.Entries {
			if g.ID == "" || ids[g.ID] || strings.TrimSpace(g.Fact) == "" || len(g.Evidence) == 0 {
				problems = append(problems, "金标条目缺少 ID/事实/证据或 ID 重复")
			}
			ids[g.ID] = true
			if g.Category != "auto" && g.Category != "sensitive" && g.Category != "temporary" {
				problems = append(problems, "金标 category 非方案枚举")
			}
			if g.Stability != "用户明确自述" && g.Stability != "多次出现的行为信号" && g.Stability != "单次弱信号" {
				problems = append(problems, "金标 stability 非方案枚举")
			}
			for _, id := range g.Evidence {
				if !messages[id] {
					problems = append(problems, "金标引用了不存在的消息")
				}
			}
		}
	}
	need := 1
	if s.ID == "U2" || s.ID == "U6" {
		need = 2
	}
	if len(annotators) < need {
		problems = append(problems, fmt.Sprintf("需要 %d 位独立人工标注者", need))
	}
	return problems
}

// ValidateConfig 要求真实价格和显式能力状态；V0 不可用是合法结论，不暗中降级为 V1。
func ValidateConfig(c Config) []string {
	var problems []string
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		problems = append(problems, "base_url 必须是无凭据、查询参数的 HTTPS API 地址")
	}
	if len(c.Models) < 2 || len(c.Models) > 3 {
		problems = append(problems, "方案要求 2–3 个模型")
	}
	if c.MaxOutputTokens < 1 || c.MaxOutputTokens > 8192 || c.TimeoutSeconds < 1 || c.TimeoutSeconds > 600 {
		problems = append(problems, "输出上限或超时不合法")
	}
	if c.PromptVersion != "v0" && c.PromptVersion != "v1" {
		problems = append(problems, "只允许原始 v0 和一次修订 v1")
	}
	variants := map[string]bool{}
	for _, v := range c.Variants {
		if variants[v] {
			problems = append(problems, "变体重复")
		}
		variants[v] = true
		if v != "V0" && v != "V1" {
			problems = append(problems, "本实现省略方案允许省略的 V2，不支持其他变体")
		}
	}
	if !variants["V0"] || !variants["V1"] {
		problems = append(problems, "必须保留 V0、V1 两个比较条件")
	}
	local, commercial := false, false
	ids := map[string]bool{}
	for _, m := range c.Models {
		if m.ID == "" || ids[m.ID] {
			problems = append(problems, "模型 ID 缺失或重复")
		}
		ids[m.ID] = true
		local = local || m.Class == "local" || m.Class == "low_cost"
		commercial = commercial || m.Class == "commercial"
		if m.Provider == "" || m.APIKeyEnv == "" || m.Currency == "" || m.PriceSource == "" {
			problems = append(problems, m.ID+": provider/API key 环境变量/币种/价格来源未填全")
		}
		if m.InputPrice == nil || m.OutputPrice == nil {
			problems = append(problems, m.ID+": 实际单价待确认，不能假定本地模型免费")
		} else if *m.InputPrice < 0 || *m.OutputPrice < 0 {
			problems = append(problems, m.ID+": 单价不能为负")
		}
		if m.APIType != "chat.completions" && m.APIType != "messages" {
			problems = append(problems, m.ID+": 不支持的 api_type")
		}
		if m.Structured != "json_schema" && m.Structured != "json_object" && m.Structured != "tool" && m.Structured != "unavailable" {
			problems = append(problems, m.ID+": V0 支持情况待核实")
		}
		if m.CapabilityEvidence == "" {
			problems = append(problems, m.ID+": 缺少 V0 能力验证证据")
		}
		if (m.APIType == "messages" && (m.Structured == "json_schema" || m.Structured == "json_object")) || (m.APIType == "chat.completions" && m.Structured == "tool") {
			problems = append(problems, m.ID+": 输出模式与协议不匹配")
		}
	}
	if !local || !commercial {
		problems = append(problems, "至少一个本地/低成本模型和一个商用强模型")
	}
	return problems
}

// ParseExtraction V1 严格按方案截取首个左花括号至末个右花括号，之后统一校验 schema 和 evidence ID。
func ParseExtraction(text, variant string, s Sample) (*Extraction, string, []string) {
	if strings.TrimSpace(text) == "" {
		return nil, "empty_output", nil
	}
	if variant == "V1" {
		a, b := strings.Index(text, "{"), strings.LastIndex(text, "}")
		if a < 0 || b < a {
			return nil, "non_json", nil
		}
		text = text[a : b+1]
	}
	if !json.Valid([]byte(text)) {
		return nil, "non_json", nil
	}
	var parsed Extraction
	decoder := json.NewDecoder(bytes.NewBufferString(text))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&parsed) != nil || parsed.Blocks == nil || parsed.Conflicts == nil {
		return nil, "schema_mismatch", nil
	}
	known := map[string]bool{}
	for _, m := range s.Messages {
		known[m.ID] = true
	}
	var invalid []string
	for i, b := range parsed.Blocks {
		if (b.Section != "沟通偏好" && b.Section != "背景与关注方向" && b.Section != "持续事项") || strings.TrimSpace(b.Text) == "" || len(b.Evidence) == 0 {
			return &parsed, "schema_mismatch", nil
		}
		for _, id := range b.Evidence {
			if !known[id] {
				invalid = append(invalid, fmt.Sprintf("block[%d]:unknown_evidence:%s", i, id))
			}
		}
	}
	for i, c := range parsed.Conflicts {
		if strings.TrimSpace(c.Topic) == "" || len(c.Statements) < 2 {
			return &parsed, "schema_mismatch", nil
		}
		for _, st := range c.Statements {
			if strings.TrimSpace(st.Text) == "" || len(st.Evidence) == 0 {
				return &parsed, "schema_mismatch", nil
			}
			for _, id := range st.Evidence {
				if !known[id] {
					invalid = append(invalid, fmt.Sprintf("conflict[%d]:unknown_evidence:%s", i, id))
				}
			}
		}
	}
	return &parsed, "", invalid
}
