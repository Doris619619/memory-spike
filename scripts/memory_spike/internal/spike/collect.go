// 文件用途：复刻方案第 5 节的分支取样、渠道排除、消息配额和近似 token 预算。
package spike

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	_ "github.com/microsoft/go-mssqldb"
)

type Row struct {
	Index    int
	Parent   int
	Role     string
	Approach string
	Content  string
}

type Session struct {
	ID      string
	Source  string
	Project bool
	Child   bool
	Updated time.Time
	Layout  string
	Rows    []Row
}

type candidate struct {
	text    string
	at      time.Time
	session string
	index   int
}

// ApproxTokens 按方案对 CJK 字符计 1/2 token、其余字符计 1/4 token；不是正式 tokenizer。
func ApproxTokens(text string) float64 {
	var quarter int
	for _, r := range text {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			quarter += 2
		} else {
			quarter++
		}
	}
	return float64(quarter) / 4
}

// EligibleSession 严格使用来源与归属元数据；缺少 source 时不猜测为普通聊天。
func EligibleSession(s Session, cutoff, timeEnd time.Time) bool {
	if s.Source != "web" || s.Project || s.Child || s.Updated.Before(cutoff) {
		return false
	}
	var layout struct {
		Type *string `json:"session_type"`
	}
	if s.Layout != "" && s.Layout != "null" {
		if json.Unmarshal([]byte(s.Layout), &layout) != nil {
			return false
		}
	}
	return layout.Type == nil || *layout.Type == "chat"
}

// branch 只回溯当前可见叶节点；环、断链、缺少叶节点或超深度均报错，不用任意最新分支代替。
func branch(s Session) ([]Row, error) {
	var layout struct {
		Displayed *int `json:"displayed_msg_idx"`
	}
	if json.Unmarshal([]byte(s.Layout), &layout) != nil || layout.Displayed == nil {
		return nil, fmt.Errorf("会话缺少 displayed_msg_idx，需补充原始布局")
	}
	byID := map[int]Row{}
	for _, r := range s.Rows {
		if _, ok := byID[r.Index]; ok {
			return nil, fmt.Errorf("消息索引重复")
		}
		byID[r.Index] = r
	}
	seen := map[int]bool{}
	out := []Row{}
	idx := *layout.Displayed
	for idx >= 0 {
		if seen[idx] {
			return nil, fmt.Errorf("消息 parent 链存在环")
		}
		if len(out) >= 2048 {
			return nil, fmt.Errorf("分支超过 2048 节点安全上限")
		}
		seen[idx] = true
		r, ok := byID[idx]
		if !ok {
			return nil, fmt.Errorf("消息 parent 链不完整")
		}
		out = append(out, r)
		idx = r.Parent
	}
	return out, nil
}

// rowCandidate 从 content JSON 的 created_at（毫秒）读取消息日期，绝不使用会话更新时间代替。
func rowCandidate(r Row, session string, start, end time.Time) (candidate, bool, bool, error) {
	if r.Role != "user" {
		return candidate{}, false, false, nil
	}
	var body struct {
		Content string   `json:"content"`
		Created *float64 `json:"created_at"`
	}
	if err := json.Unmarshal([]byte(r.Content), &body); err != nil {
		return candidate{}, false, false, fmt.Errorf("用户消息 content JSON 不合法")
	}
	if body.Created == nil {
		return candidate{}, false, false, fmt.Errorf("用户消息缺少 content.created_at")
	}
	at := time.UnixMilli(int64(*body.Created)).UTC()
	if at.Before(start) || at.After(end) || strings.HasPrefix(strings.TrimSpace(body.Content), "<agent-message") || strings.TrimSpace(body.Content) == "" {
		return candidate{}, false, false, nil
	}
	runes := []rune(body.Content)
	truncated := len(runes) > 2000
	if truncated {
		runes = runes[:2000]
	}
	return candidate{text: string(runes), at: at, session: session, index: r.Index}, true, truncated, nil
}

// Collect 遵循最近优先选 50 会话、每会话先取 15 条，再按全局最近顺序补到 500 条和约 24k token。
func Collect(sessions []Session, id, provenance string, consented bool, end time.Time) (Sample, error) {
	start := end.AddDate(0, 0, -30)
	result := Sample{ID: id, WindowStart: start.UTC().Format(time.RFC3339), WindowEnd: end.UTC().Format(time.RFC3339), Provenance: provenance, Consented: consented, Messages: []Message{}}
	if provenance == "real" && !consented {
		return result, fmt.Errorf("真实用户样本必须先取得知情同意")
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		if sessions[i].Updated.Equal(sessions[j].Updated) {
			return sessions[i].ID < sessions[j].ID
		}
		return sessions[i].Updated.After(sessions[j].Updated)
	})
	var selected [][]candidate
	truncation := false
	for _, s := range sessions {
		if !EligibleSession(s, start, end) {
			continue
		}
		rows, err := branch(s)
		if err != nil {
			return result, err
		}
		campus := false
		for _, r := range rows {
			if r.Approach == "campus-affairs-qa" {
				campus = true
				break
			}
		}
		if campus {
			continue
		}
		result.EligibleSessions++
		cs := []candidate{}
		for _, r := range rows {
			c, ok, trunc, err := rowCandidate(r, s.ID, start, end)
			if err != nil {
				return result, err
			}
			if ok {
				cs = append(cs, c)
				if trunc && len(selected) < 50 {
					truncation = true
				}
			}
		}
		result.EligibleMessages += len(cs)
		if len(selected) >= 50 {
			result.Sampled = true
			continue
		}
		sort.SliceStable(cs, func(i, j int) bool { return newer(cs[i], cs[j]) })
		selected = append(selected, cs)
	}
	var first, overflow []candidate
	for _, cs := range selected {
		n := min(15, len(cs))
		first = append(first, cs[:n]...)
		overflow = append(overflow, cs[n:]...)
	}
	sort.SliceStable(first, func(i, j int) bool { return newer(first[i], first[j]) })
	sort.SliceStable(overflow, func(i, j int) bool { return newer(overflow[i], overflow[j]) })
	kept := []candidate{}
	seen := map[string]bool{}
	for _, c := range append(first, overflow...) {
		key := fmt.Sprintf("%s:%d", c.session, c.index)
		if seen[key] {
			continue
		}
		seen[key] = true
		tokens := ApproxTokens(c.text)
		if len(kept) >= 500 || result.ApproxTokens+tokens > 24000 {
			result.Sampled = true
			continue
		}
		kept = append(kept, c)
		result.ApproxTokens += tokens
	}
	sort.SliceStable(kept, func(i, j int) bool { return newer(kept[j], kept[i]) })
	aliases := map[string]string{}
	for i, c := range kept {
		alias, ok := aliases[c.session]
		if !ok {
			alias = fmt.Sprintf("s_%02d", len(aliases)+1)
			aliases[c.session] = alias
		}
		result.Messages = append(result.Messages, Message{ID: fmt.Sprintf("m_%03d", i+1), Session: alias, Date: c.at.Format(time.RFC3339), Text: c.text})
	}
	result.SessionCount = len(aliases)
	result.MessageCount = len(result.Messages)
	result.Sampled = result.Sampled || truncation
	if truncation {
		result.Warnings = append(result.Warnings, "部分消息按 2000 字符上限截断")
	}
	return result, nil
}

// newer 为相同时间添加稳定排序键，使相同输入能产生相同匿名编号和摘要。
func newer(a, b candidate) bool {
	if !a.at.Equal(b.at) {
		return a.at.After(b.at)
	}
	if a.session != b.session {
		return a.session < b.session
	}
	return a.index > b.index
}

// ReadDatabase 只执行带 owner 参数的 SELECT；连接使用只读账号，错误不回显 DSN 或用户身份。
func ReadDatabase(ctx context.Context, dsn, owner string, end time.Time) ([]Session, error) {
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return nil, fmt.Errorf("无法初始化 SQL Server 连接")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	rows, err := db.QueryContext(ctx, `SELECT chat_session_id, layout, updated_at FROM chat_session
WHERE user_id = @owner AND source = 'web' AND project_id IS NULL AND parent_session_id IS NULL
AND (JSON_VALUE(layout, '$.session_type') = 'chat' OR JSON_VALUE(layout, '$.session_type') IS NULL)
AND updated_at >= @cutoff ORDER BY updated_at DESC, chat_session_id`, sql.Named("owner", owner), sql.Named("cutoff", end.AddDate(0, 0, -30)))
	if err != nil {
		return nil, fmt.Errorf("会话查询失败；请检查只读连接与表结构")
	}
	sessions := []Session{}
	for rows.Next() {
		var s Session
		var layout sql.NullString
		if err := rows.Scan(&s.ID, &layout, &s.Updated); err != nil {
			rows.Close()
			return nil, fmt.Errorf("会话字段解析失败")
		}
		s.Layout = layout.String
		s.Source = "web"
		sessions = append(sessions, s)
		if len(sessions) > 5000 {
			rows.Close()
			return nil, fmt.Errorf("会话超过安全读取上限，请人工审查后调整")
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, fmt.Errorf("会话读取中断")
	}
	for i := range sessions {
		msgs, err := db.QueryContext(ctx, `SELECT TOP (10001) m.self_idx, m.parent_idx, m.role, m.approach_id, m.content FROM chat_message m
JOIN chat_session s ON s.chat_session_id=m.chat_session_id WHERE s.user_id=@owner AND s.chat_session_id=@session`, sql.Named("owner", owner), sql.Named("session", sessions[i].ID))
		if err != nil {
			return nil, fmt.Errorf("消息查询失败")
		}
		for msgs.Next() {
			var r Row
			var app sql.NullString
			if err := msgs.Scan(&r.Index, &r.Parent, &r.Role, &app, &r.Content); err != nil {
				msgs.Close()
				return nil, fmt.Errorf("消息字段解析失败")
			}
			r.Approach = app.String
			sessions[i].Rows = append(sessions[i].Rows, r)
		}
		err = msgs.Err()
		msgs.Close()
		if err != nil {
			return nil, fmt.Errorf("消息读取中断")
		}
		if len(sessions[i].Rows) > 10000 {
			return nil, fmt.Errorf("单会话超过安全读取上限")
		}
	}
	return sessions, nil
}
