// 文件用途：生成明确标为 Codex 来源、仅限本地核对的单人样本；不调用模型或进入正式六样本统计。
package spike

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type CodexStats struct {
	codexReadStats
	FilesConsidered   int            `json:"files_considered"`
	HumanFiles        int            `json:"human_files"`
	ExcludedFiles     map[string]int `json:"excluded_files_by_reason"`
	DuplicateMessages int            `json:"duplicate_messages"`
	AmbiguousMessages int            `json:"excluded_ambiguous_message_ids"`
	RedactedMessages  int            `json:"redacted_messages_before_sampling"`
	RedactedSpans     int            `json:"redacted_spans_before_sampling"`
	TruncatedMessages int            `json:"truncated_messages_before_sampling"`
}

type CodexPreview struct {
	Description              string     `json:"description"`
	DatasetType              string     `json:"dataset_type"`
	Source                   string     `json:"source"`
	ParticipantCount         int        `json:"participant_count"`
	LocalProcessingConsent   bool       `json:"local_processing_consent"`
	ModelTransmissionConsent bool       `json:"model_transmission_consent"`
	FormalExperimentEligible bool       `json:"formal_experiment_eligible"`
	Sample                   Sample     `json:"sample"`
	SampleHash               string     `json:"sample_sha256"`
	Stats                    CodexStats `json:"collection_stats"`
	Limitations              []string   `json:"limitations"`
}

// FindCodexFiles 只枚举显式给定目录的 JSONL；不读取数据库、附件或其它应用，拒绝根路径符号链接。
func FindCodexFiles(roots []string, start time.Time) ([]string, error) {
	found := map[string]bool{}
	for _, root := range roots {
		info, err := os.Lstat(root)
		if err != nil {
			return nil, fmt.Errorf("输入目录不可访问")
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("输入必须是普通目录")
		}
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return fmt.Errorf("输入目录枚举失败")
			}
			if d.IsDir() || d.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(d.Name(), ".jsonl") {
				return nil
			}
			stat, err := d.Info()
			if err != nil {
				return fmt.Errorf("无法检查记录文件")
			}
			if stat.ModTime().Before(start) {
				return nil
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			found[abs] = true
			if len(found) > 3000 {
				return fmt.Errorf("超过 3000 文件安全上限，请缩小明确的输入目录范围")
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	paths := make([]string, 0, len(found))
	for p := range found {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths, nil
}

// BuildCodexPreview 以消息原始时间截窗；先全局去重和脱敏，再复用 50/500/24k 上限，不伪造 SQL 元数据。
func BuildCodexPreview(paths []string, end time.Time, consent bool) (CodexPreview, error) {
	if !consent {
		return CodexPreview{}, fmt.Errorf("需要本人同意本地读取与处理")
	}
	end = end.UTC().Truncate(time.Second)
	start := end.AddDate(0, 0, -30)
	p := CodexPreview{Description: "本人 Codex 聊天的本地预实验样本，尚未批准传输给模型；不是学校平台正式 U1–U6 样本。", DatasetType: "codex_local_preview", Source: "codex-local", ParticipantCount: 1, LocalProcessingConsent: true, Stats: CodexStats{ExcludedFiles: map[string]int{}}, Limitations: []string{"仅覆盖给定目录中仍存在、最近修改且可解析的记录，不代表完整聊天历史。", "保守排除缺少来源标记或无法确认由本人发起的消息；编辑分支不等同于学校 displayed_msg_idx。", "只保留文字；不读取附件、图片、音频或工具输出。", "正则脱敏不是全面匿名化，姓名、住址、健康和第三方资料仍需本人逐条检查。", "同一人只算一位参与者；不计入学校正式六样本质量统计。", "未调用模型，未生成或伪造人工金标。"}}
	p.Sample = Sample{ID: "C1", Provenance: "real", WindowStart: start.Format(time.RFC3339), WindowEnd: end.Format(time.RFC3339), Messages: []Message{}}
	byID := map[string]codexInput{}
	ambiguous := map[string]bool{}
	sort.Strings(paths)
	for _, path := range paths {
		p.Stats.FilesConsidered++
		f, err := readCodexRollout(path, start, end)
		if err != nil {
			p.Stats.ExcludedFiles[err.Error()]++
			continue
		}
		if !f.isHuman {
			p.Stats.ExcludedFiles["not_explicit_user_thread"]++
			continue
		}
		p.Stats.HumanFiles++
		p.Stats.UserRecords += f.stats.UserRecords
		p.Stats.NonHumanTurns += f.stats.NonHumanTurns
		p.Stats.MissingMetadata += f.stats.MissingMetadata
		p.Stats.OutsideWindow += f.stats.OutsideWindow
		p.Stats.AutoContent += f.stats.AutoContent
		for _, m := range f.messages {
			if old, ok := byID[m.id]; ok {
				if old.text != m.text || !old.at.Equal(m.at) {
					ambiguous[m.id] = true
				} else {
					p.Stats.DuplicateMessages++
				}
				continue
			}
			byID[m.id] = m
		}
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	bySession := map[string][]candidate{}
	latest := map[string]time.Time{}
	seenCopy := map[string]bool{}
	for _, id := range ids {
		if ambiguous[id] {
			p.Stats.AmbiguousMessages++
			continue
		}
		m := byID[id]
		copyKey := Hash([]byte(m.at.Format(time.RFC3339Nano) + "\x00" + m.text))
		if seenCopy[copyKey] {
			p.Stats.DuplicateMessages++
			continue
		}
		seenCopy[copyKey] = true
		text, n := redactCodexText(m.text)
		if n > 0 {
			p.Stats.RedactedMessages++
			p.Stats.RedactedSpans += n
		}
		runes := []rune(text)
		if len(runes) > 2000 {
			text = string(runes[:2000])
			p.Stats.TruncatedMessages++
		}
		bySession[m.session] = append(bySession[m.session], candidate{text: text, at: m.at, session: m.session, index: len(bySession[m.session])})
		if latest[m.session].Before(m.at) {
			latest[m.session] = m.at
		}
	}
	sessions := make([]string, 0, len(bySession))
	for id := range bySession {
		sessions = append(sessions, id)
	}
	sort.Slice(sessions, func(i, j int) bool {
		if latest[sessions[i]].Equal(latest[sessions[j]]) {
			return sessions[i] < sessions[j]
		}
		return latest[sessions[i]].After(latest[sessions[j]])
	})
	p.Sample.EligibleSessions = len(sessions)
	selected := [][]candidate{}
	for i, id := range sessions {
		messages := bySession[id]
		p.Sample.EligibleMessages += len(messages)
		if i >= 50 {
			p.Sample.Sampled = true
			continue
		}
		sort.Slice(messages, func(i, j int) bool { return newer(messages[i], messages[j]) })
		selected = append(selected, messages)
	}
	applySampleLimits(&p.Sample, selected, p.Stats.TruncatedMessages > 0)
	if p.Sample.MessageCount == 0 {
		return p, fmt.Errorf("没有可确认的本人文字消息；请检查本地格式与时间窗，不以猜测字段替代")
	}
	p.SampleHash = SampleHash(p.Sample)
	return p, nil
}

// WriteCodexPreview 创建全新私人目录中的 JSON、可读样本和空金标；不输出源路径、原始账号或会话 ID。
func WriteCodexPreview(p CodexPreview, dir string) error {
	if err := PrivatePath(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0700); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		return fmt.Errorf("输出目录必须不存在；拒绝覆盖既有样本")
	}
	if err := WriteJSON(filepath.Join(dir, "preview.json"), p); err != nil {
		return err
	}
	gold := Annotation{SampleID: p.Sample.ID, SampleHash: p.SampleHash, Entries: []Gold{}}
	if err := WriteJSON(filepath.Join(dir, "gold.blank.json"), gold); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("<!-- 文件用途：供本人检查预实验输入及隐私；不是模型输出，也不是人工金标。 -->\n# 你的 Codex 聊天样本（仅本地）\n\n")
	fmt.Fprintf(&b, "来源：Codex；参与者：1 人。时间窗：%s 至 %s（UTC）。\n\n选中 %d 个会话、%d 条消息，约 %.0f token。样本摘要：`%s`。\n\n", p.Sample.WindowStart, p.Sample.WindowEnd, p.Sample.SessionCount, p.Sample.MessageCount, p.Sample.ApproxTokens, p.SampleHash)
	b.WriteString("这份文字尚未发送到学校 API。请先检查是否有不愿发送的内容；密码/邮箱等自动遮盖不代表所有隐私都已清除。\n\n如果要去掉某条，告诉代理 m_编号，重新生成样本及摘要；不要直接修改 JSON 后仍沿用旧摘要。金标请独立填写 gold.blank.json，不能把代理建议当成人工答案。\n\n")
	b.WriteString("## 输入文字\n\n")
	for _, m := range p.Sample.Messages {
		fmt.Fprintf(&b, "### %s · %s · %s\n\n", m.ID, m.Session, m.Date)
		// 四空格缩进使原文中的链接、HTML 和指令成为静态文本，避免预览时加载外部资源。
		for _, line := range strings.Split(m.Text, "\n") {
			b.WriteString("    " + line + "\n")
		}
		b.WriteString("\n")
	}
	if err := WriteNew(filepath.Join(dir, "sample-review.md"), []byte(b.String())); err != nil {
		return err
	}
	summary := struct {
		Description string     `json:"description"`
		Hash        string     `json:"sample_sha256"`
		Stats       CodexStats `json:"stats"`
		Limitations []string   `json:"limitations"`
	}{"只包含取样计数与边界的本地审计记录", p.SampleHash, p.Stats, p.Limitations}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	return WriteNew(filepath.Join(dir, "collection-summary.json"), append(data, '\n'))
}
