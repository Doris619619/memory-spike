// 文件用途：只读解析 Codex JSONL，依据用户输入标记、发起来源和消息原始时间保守取样。
package spike

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"time"
)

type codexRecord struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}
type codexInput struct {
	id, turn, session, text string
	at                      time.Time
}
type codexReadStats struct {
	UserRecords     int `json:"user_records_seen"`
	NonHumanTurns   int `json:"excluded_nonhuman_or_unverified_turns"`
	MissingMetadata int `json:"excluded_missing_message_metadata"`
	OutsideWindow   int `json:"excluded_outside_window"`
	AutoContent     int `json:"excluded_automatic_or_unsupported_content"`
}
type codexFile struct {
	id       string
	isHuman  bool
	messages []codexInput
	stats    codexReadStats
}

// readCodexRollout 读取打开时的文件大小快照；不追随增长，损坏/超长行导致整文件拒绝且不返回半份数据。
func readCodexRollout(path string, start, end time.Time) (codexFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return codexFile{}, fmt.Errorf("open_failed")
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return codexFile{}, fmt.Errorf("stat_failed")
	}
	if stat.Size() > 128<<20 {
		return codexFile{}, fmt.Errorf("file_over_128_mib")
	}
	return parseCodexRollout(io.LimitReader(f, stat.Size()), start, end)
}

// parseCodexRollout 要求 thread_source=user、source 为正常客户端，并仅采纳明确由人发起的 turn。
func parseCodexRollout(reader io.Reader, start, end time.Time) (codexFile, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 64<<20)
	out := codexFile{}
	allowed := map[string]bool{}
	seenTurn := map[string]bool{}
	pending := []codexInput{}
	metadataSeen := false
	for scanner.Scan() {
		if len(strings.TrimSpace(scanner.Text())) == 0 {
			continue
		}
		var record codexRecord
		if json.Unmarshal(scanner.Bytes(), &record) != nil {
			return codexFile{}, fmt.Errorf("malformed_jsonl")
		}
		switch record.Type {
		case "session_meta":
			var meta struct {
				ID           string          `json:"id"`
				Source       json.RawMessage `json:"source"`
				ThreadSource string          `json:"thread_source"`
			}
			if json.Unmarshal(record.Payload, &meta) != nil || meta.ID == "" {
				return codexFile{}, fmt.Errorf("invalid_session_metadata")
			}
			var source string
			human := json.Unmarshal(meta.Source, &source) == nil && (source == "vscode" || source == "cli" || source == "exec") && meta.ThreadSource == "user"
			if metadataSeen && (meta.ID != out.id || human != out.isHuman) {
				return codexFile{}, fmt.Errorf("inconsistent_session_metadata")
			}
			metadataSeen = true
			out.id = meta.ID
			out.isHuman = human
			if !human {
				return out, nil
			}
		case "event_msg":
			var event struct {
				Type        string `json:"type"`
				Turn        string `json:"turn_id"`
				Attribution *struct {
					Trigger   string  `json:"turn_trigger"`
					Initiator *string `json:"initiating_agent_path"`
					Parent    *string `json:"parent_turn_id"`
				} `json:"turn_attribution"`
			}
			if json.Unmarshal(record.Payload, &event) != nil {
				return codexFile{}, fmt.Errorf("invalid_event")
			}
			if event.Type == "task_started" && event.Turn != "" {
				a := event.Attribution
				human := a != nil && a.Initiator == nil && a.Parent == nil && (a.Trigger == "composer" || a.Trigger == "edit_user_message" || a.Trigger == "remote_ios")
				if seenTurn[event.Turn] {
					allowed[event.Turn] = allowed[event.Turn] && human
				} else {
					allowed[event.Turn] = human
				}
				seenTurn[event.Turn] = true
			}
		case "response_item":
			var item struct {
				Type    string `json:"type"`
				Role    string `json:"role"`
				ID      string `json:"id"`
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
			if json.Unmarshal(record.Payload, &item) != nil {
				return codexFile{}, fmt.Errorf("invalid_response_item")
			}
			if item.Role != "user" || item.Type != "message" {
				continue
			}
			out.stats.UserRecords++
			if item.Meta == nil || item.Meta.Created == nil || item.Meta.Turn == "" || item.ID == "" || len(item.Meta.Kinds) != len(item.Content) {
				out.stats.MissingMetadata++
				continue
			}
			created := *item.Meta.Created
			if math.IsNaN(created) || math.IsInf(created, 0) || created < 0 || created > 1e11 {
				out.stats.MissingMetadata++
				continue
			}
			at := time.UnixMilli(int64(math.Round(created * 1000))).UTC()
			if at.Before(start) || at.After(end) {
				out.stats.OutsideWindow++
				continue
			}
			parts := []string{}
			for i, part := range item.Content {
				if item.Meta.Kinds[i] != "user.text" || part.Type != "input_text" {
					out.stats.AutoContent++
					continue
				}
				text, ok := cleanCodexText(part.Text)
				if !ok {
					out.stats.AutoContent++
					continue
				}
				parts = append(parts, text)
			}
			if len(parts) == 0 {
				continue
			}
			pending = append(pending, codexInput{id: item.ID, turn: item.Meta.Turn, text: strings.Join(parts, "\n"), at: at})
		}
	}
	if scanner.Err() != nil {
		return codexFile{}, fmt.Errorf("incomplete_or_oversized_line")
	}
	if !metadataSeen {
		return codexFile{}, fmt.Errorf("missing_session_metadata")
	}
	for _, message := range pending {
		if !allowed[message.turn] {
			out.stats.NonHumanTurns++
			continue
		}
		message.session = out.id
		out.messages = append(out.messages, message)
	}
	return out, nil
}
