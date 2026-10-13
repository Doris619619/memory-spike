// 文件用途：用虚构日志验证 Codex 取样的来源隔离、原始时间、去重、脱敏和正式实验隔离。
package spike

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// codexFixture 生成单个虚构用户线程日志；内容与 ID 不对应真实用户。
func codexFixture(session, source, trigger, id, text string, at time.Time) string {
	records := []map[string]any{
		{"type": "session_meta", "payload": map[string]any{"id": session, "source": "vscode", "thread_source": source}},
		{"type": "event_msg", "payload": map[string]any{"type": "task_started", "turn_id": "turn-test", "turn_attribution": map[string]any{"turn_trigger": trigger, "parent_turn_id": nil, "initiating_agent_path": nil}}},
		{"type": "response_item", "timestamp": at.AddDate(0, 0, 40).Format(time.RFC3339), "payload": map[string]any{"type": "message", "role": "user", "id": id, "content": []any{map[string]any{"type": "input_text", "text": text}}, "internal_chat_message_metadata_passthrough": map[string]any{"turn_id": "turn-test", "create_time": float64(at.UnixMilli()) / 1000, "content_item_kinds": []string{"user.text"}}}},
	}
	var b strings.Builder
	for _, r := range records {
		data, _ := json.Marshal(r)
		b.Write(data)
		b.WriteByte('\n')
	}
	return b.String()
}

// writeCodexFixture 只写测试临时目录，返回明确的输入文件路径。
func writeCodexFixture(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestCodexUsesOriginalTimestamp 对比保存时间与真实消息时间，防止复制旧历史冒充近期聊天。
func TestCodexUsesOriginalTimestamp(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	for _, age := range []int{2, 40} {
		f, err := parseCodexRollout(strings.NewReader(codexFixture("s", "user", "composer", "m", "我偏好先看例子", now.AddDate(0, 0, -age))), now.AddDate(0, 0, -30), now)
		if err != nil {
			t.Fatal(err)
		}
		if age == 2 && len(f.messages) != 1 {
			t.Fatal("recent human message lost")
		}
		if age == 40 && len(f.messages) != 0 {
			t.Fatal("copied old message included")
		}
	}
}

// TestCodexRejectsAgentAndUnknownTurns 缺少真人发起证据或子代理线程时不猜测为本人输入。
func TestCodexRejectsAgentAndUnknownTurns(t *testing.T) {
	now := time.Now().UTC()
	for _, pair := range [][2]string{{"subagent", "composer"}, {"user", "resume_interrupted_task"}, {"user", "unknown"}} {
		f, err := parseCodexRollout(strings.NewReader(codexFixture("s", pair[0], pair[1], "m", "自动续跑消息", now)), now.AddDate(0, 0, -30), now)
		if err != nil || len(f.messages) != 0 {
			t.Fatal("nonhuman accepted", err)
		}
	}
	data := strings.Replace(codexFixture("s", "user", "composer", "m", "跨代理传来的任务", now), `"initiating_agent_path":null`, `"initiating_agent_path":"/root"`, 1)
	f, err := parseCodexRollout(strings.NewReader(data), now.AddDate(0, 0, -30), now)
	if err != nil || len(f.messages) != 0 {
		t.Fatal("agent initiation accepted", err)
	}
}

// TestCodexUsesContentKindsAndRejectsMalformedMetadata 只接受 user.text，缺失标记不回退到 role=user。
func TestCodexUsesContentKindsAndRejectsMalformedMetadata(t *testing.T) {
	now := time.Now().UTC()
	raw := codexFixture("s", "user", "composer", "m", "不能把系统提示当本人", now)
	for _, value := range []string{`["environments.environment_context"]`, `[]`} {
		data := strings.Replace(raw, `["user.text"]`, value, 1)
		f, err := parseCodexRollout(strings.NewReader(data), now.AddDate(0, 0, -30), now)
		if err != nil || len(f.messages) != 0 {
			t.Fatal("unmarked context accepted", err)
		}
	}
}

// TestCodexCleansWrappersButKeepsRequest 保留附件包装后的本人请求，不读附件或把页面上下文当偏好。
func TestCodexCleansWrappersButKeepsRequest(t *testing.T) {
	text, ok := cleanCodexText("# Files mentioned by the user:\nsecret.txt\n## My request:\n请用中文解释")
	if !ok || text != "请用中文解释" {
		t.Fatal("request wrapper not stripped")
	}
	for _, text := range []string{"<environment_context>自动注入</environment_context>", "<agent-message>其它代理</agent-message>", "# AGENTS.md instructions for test", "<send_user_message_question_reply>[]</send_user_message_question_reply>"} {
		if _, ok := cleanCodexText(text); ok {
			t.Fatal("automatic wrapper accepted")
		}
	}
}

// TestCodexRedactsDeviceLocators 遮盖内部 IP 和 Windows 用户目录名，保留其它文字供人工检查。
func TestCodexRedactsDeviceLocators(t *testing.T) {
	text, count := redactCodexText(`ssh example@100.70.7.29；目录 C:\Users\ExamplePerson\project`)
	if count != 2 || strings.Contains(text, "100.70.7.29") || strings.Contains(text, "ExamplePerson") {
		t.Fatal("device locator leaked")
	}
}

// TestCodexRedactsBeforeTruncation 密钥即使横跨 2000 字符截断点也不会保留前缀片段。
func TestCodexRedactsBeforeTruncation(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	secret := "sk-" + strings.Repeat("x", 25)
	input := strings.Repeat("中", 1990) + secret + " 密码是 123456，邮箱 test@example.invalid"
	path := writeCodexFixture(t, codexFixture("s", "user", "composer", "m", input, now))
	p, err := BuildCodexPreview([]string{path}, now, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(p.Sample.Messages[0].Text, "sk-") || p.Stats.RedactedSpans != 3 || len([]rune(p.Sample.Messages[0].Text)) != 2000 {
		t.Fatal("redaction/truncation failed")
	}
}

// TestCodexDeduplicatesCopiesWithoutCollapsingNewRepetition 相同原始时间/正文的历史副本只保留一次，真正再次输入保留。
func TestCodexDeduplicatesCopiesWithoutCollapsingNewRepetition(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	at := now.Add(-time.Hour)
	paths := []string{
		writeCodexFixture(t, codexFixture("s1", "user", "composer", "m1", "偏好中文", at)),
		writeCodexFixture(t, codexFixture("s2", "user", "composer", "m1", "偏好中文", at)),
		writeCodexFixture(t, codexFixture("s3", "user", "composer", "m2", "偏好中文", at)),
		writeCodexFixture(t, codexFixture("s1", "user", "composer", "m3", "偏好中文", now)),
	}
	p, err := BuildCodexPreview(paths, now, true)
	if err != nil {
		t.Fatal(err)
	}
	if p.Sample.MessageCount != 2 || p.Stats.DuplicateMessages != 2 {
		t.Fatal("duplicate history counted as repetition")
	}
}

// TestCodexExcludesConflictingIDsAndBrokenFiles 不从冲突 ID 或损坏日志中静默选择部分事实。
func TestCodexExcludesConflictingIDsAndBrokenFiles(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	paths := []string{
		writeCodexFixture(t, codexFixture("s1", "user", "composer", "m1", "旧版偏好", now)),
		writeCodexFixture(t, codexFixture("s1", "user", "composer", "m1", "编辑后的不同偏好", now)),
		writeCodexFixture(t, codexFixture("s2", "user", "composer", "m2", "完整消息", now)+`{"broken":`),
		writeCodexFixture(t, codexFixture("s3", "user", "composer", "m3", "可靠保留", now)),
	}
	p, err := BuildCodexPreview(paths, now, true)
	if err != nil {
		t.Fatal(err)
	}
	if p.Sample.MessageCount != 1 || p.Stats.AmbiguousMessages != 1 || p.Stats.ExcludedFiles["malformed_jsonl"] != 1 {
		t.Fatal("ambiguous/partial data accepted")
	}
}

// TestCodexPreviewRemainsLocalAndSeparate 写出的预览不含源 ID；缺本地授权拒绝，正式矩阵仍拒绝 C1。
func TestCodexPreviewRemainsLocalAndSeparate(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	paths := []string{writeCodexFixture(t, codexFixture("original-private-session", "user", "composer", "original-private-message", "请先讲直觉", now))}
	if _, err := BuildCodexPreview(paths, now, false); err == nil {
		t.Fatal("missing local consent accepted")
	}
	p, err := BuildCodexPreview(paths, now, true)
	if err != nil {
		t.Fatal(err)
	}
	if p.ModelTransmissionConsent || p.FormalExperimentEligible || p.Sample.Consented || len(ValidateSample(p.Sample)) == 0 {
		t.Fatal("preview allowed formal run")
	}
	dir := filepath.Join(t.TempDir(), "preview")
	if err := WriteCodexPreview(p, dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "preview.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "original-private-") {
		t.Fatal("source IDs leaked")
	}
	if err := WriteCodexPreview(p, dir); err == nil {
		t.Fatal("existing review overwritten")
	}
}
