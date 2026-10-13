// 文件用途：只读发现历史 Codex 候选原话，显式保留旧格式来源不确定性；不替代正式取样。
package spike

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

type DiscoveryMessage struct {
	ID      string `json:"id"`
	Session string `json:"session"`
	Date    string `json:"date"`
	Text    string `json:"text"`
	Basis   string `json:"source_basis"`
}
type Discovery struct {
	Description string             `json:"description"`
	Messages    []DiscoveryMessage `json:"messages"`
	Files       int                `json:"files_considered"`
	Excluded    map[string]int     `json:"excluded"`
}

// DiscoverCodex 扩大至给定目录的全部可读历史；去重、脱敏后输出候选，不生成金标或质量分数。
func DiscoverCodex(paths []string) Discovery {
	result := Discovery{Description: "历史探索候选；旧格式未能完全确认真人发起，须本人审计；不是原方案的30天取样。", Messages: []DiscoveryMessage{}, Files: len(paths), Excluded: map[string]int{}}
	seen := map[string]bool{}
	sort.Strings(paths)
	for _, path := range paths {
		messages, err := readDiscovery(path)
		if err != nil {
			result.Excluded[err.Error()]++
			continue
		}
		for _, m := range messages {
			key := Hash([]byte(m.Date + "\x00" + m.Text))
			if seen[key] {
				result.Excluded["duplicate_message"]++
				continue
			}
			seen[key] = true
			m.ID = "d_" + key[:16]
			result.Messages = append(result.Messages, m)
		}
	}
	sort.Slice(result.Messages, func(i, j int) bool { return result.Messages[i].Date < result.Messages[j].Date })
	return result
}

// readDiscovery 拒绝子代理线程和已知自动 turn；旧日志使用记录时间并标明非原始消息时间。
func readDiscovery(path string) ([]DiscoveryMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open_failed")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat_failed")
	}
	if info.Size() > 128<<20 {
		return nil, fmt.Errorf("over_128_mib")
	}
	return parseDiscovery(io.LimitReader(f, info.Size()))
}

// parseDiscovery 保留普通用户文字候选；缺少新格式元数据时只标记为旧格式候选而不冒充已确认来源。
func parseDiscovery(reader io.Reader) ([]DiscoveryMessage, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 64<<20)
	var output []DiscoveryMessage
	session := ""
	activeTurn := ""
	turns := map[string]string{}
	for scanner.Scan() {
		var r struct {
			Type      string          `json:"type"`
			Timestamp string          `json:"timestamp"`
			Payload   json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(scanner.Bytes(), &r) != nil {
			return nil, fmt.Errorf("malformed_jsonl")
		}
		switch r.Type {
		case "session_meta":
			var m struct {
				ID     string          `json:"id"`
				Source json.RawMessage `json:"source"`
				Thread string          `json:"thread_source"`
			}
			if json.Unmarshal(r.Payload, &m) != nil {
				return nil, fmt.Errorf("invalid_metadata")
			}
			var source string
			if json.Unmarshal(m.Source, &source) != nil || (source != "vscode" && source != "cli" && source != "exec") || m.Thread != "user" {
				return nil, fmt.Errorf("not_user_thread")
			}
			if session != "" && session != m.ID {
				return nil, fmt.Errorf("inconsistent_session")
			}
			session = m.ID
		case "event_msg":
			var e struct {
				Type        string `json:"type"`
				Turn        string `json:"turn_id"`
				Attribution *struct {
					Trigger   string  `json:"turn_trigger"`
					Initiator *string `json:"initiating_agent_path"`
					Parent    *string `json:"parent_turn_id"`
				} `json:"turn_attribution"`
			}
			if json.Unmarshal(r.Payload, &e) != nil {
				return nil, fmt.Errorf("invalid_event")
			}
			if e.Type == "task_started" {
				activeTurn = e.Turn
				basis := "legacy_unverified"
				if e.Attribution != nil {
					a := e.Attribution
					basis = "excluded"
					if a.Initiator == nil && a.Parent == nil && (a.Trigger == "composer" || a.Trigger == "edit_user_message" || a.Trigger == "remote_ios") {
						basis = "human_trigger"
					}
				}
				if turns[e.Turn] != "excluded" {
					turns[e.Turn] = basis
				}
			}
		case "response_item":
			var m struct {
				Type    string `json:"type"`
				Role    string `json:"role"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
				Meta *struct {
					Turn    string   `json:"turn_id"`
					Created *float64 `json:"create_time"`
					Kinds   []string `json:"content_item_kinds"`
				} `json:"internal_chat_message_metadata_passthrough"`
			}
			if json.Unmarshal(r.Payload, &m) != nil {
				return nil, fmt.Errorf("invalid_message")
			}
			if m.Type != "message" || m.Role != "user" || session == "" {
				continue
			}
			turn := activeTurn
			if m.Meta != nil && m.Meta.Turn != "" {
				turn = m.Meta.Turn
			}
			if turns[turn] == "excluded" {
				continue
			}
			basis := "legacy_user_role_log_time_unverified"
			at, err := time.Parse(time.RFC3339Nano, r.Timestamp)
			if err != nil {
				continue
			}
			typed := m.Meta != nil && len(m.Meta.Kinds) == len(m.Content) && len(m.Content) > 0
			if m.Meta != nil && m.Meta.Created != nil && *m.Meta.Created > 0 && *m.Meta.Created < 1e11 {
				at = time.UnixMilli(int64(*m.Meta.Created * 1000)).UTC()
				basis = "user_role_original_time_unverified"
				if typed && turns[turn] == "human_trigger" {
					basis = "explicit_human_user_text"
				}
			}
			parts := []string{}
			for i, c := range m.Content {
				if c.Type != "input_text" || (typed && m.Meta.Kinds[i] != "user.text") {
					continue
				}
				text, ok := cleanCodexText(c.Text)
				if !ok {
					continue
				}
				if strings.HasPrefix(text, "PLEASE IMPLEMENT THIS PLAN:") || strings.HasPrefix(text, "# In app browser:") {
					continue
				}
				text, _ = redactCodexText(text)
				if len([]rune(text)) > 4000 {
					continue
				}
				parts = append(parts, text)
			}
			if len(parts) == 0 {
				continue
			}
			text := strings.Join(parts, "\n")
			if len([]rune(text)) > 4000 {
				continue
			}
			output = append(output, DiscoveryMessage{Session: "session_" + Hash([]byte(session))[:12], Date: at.UTC().Format(time.RFC3339Nano), Text: text, Basis: basis})
		}
	}
	if scanner.Err() != nil {
		return nil, fmt.Errorf("incomplete_or_oversized_line")
	}
	return output, nil
}
