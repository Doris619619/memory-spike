// 文件用途：验证探索候选的旧格式标识、子代理排除、凭据遮盖和 AI 审阅的正式实验隔离。
package spike

import (
	"strings"
	"testing"
	"time"
)

// TestDiscoverySources 区分明确用户输入与旧格式候选，自动 turn 和子代理保持排除。
func TestDiscoverySources(t *testing.T) {
	now := time.Now().UTC()
	for _, trigger := range []string{"composer", "resume_interrupted_task"} {
		result, err := parseDiscovery(strings.NewReader(codexFixture("s", "user", trigger, "m", "我喜欢清晰的讲解", now)))
		if err != nil {
			t.Fatal(err)
		}
		if trigger == "composer" && (len(result) != 1 || result[0].Basis != "explicit_human_user_text") {
			t.Fatal("lost explicit source")
		}
		if trigger != "composer" && len(result) != 0 {
			t.Fatal("automatic input accepted")
		}
	}
	if _, err := parseDiscovery(strings.NewReader(codexFixture("s", "subagent", "composer", "m", "text", now))); err == nil {
		t.Fatal("subagent accepted")
	}
	legacy := `{"type":"session_meta","payload":{"id":"s","source":"vscode","thread_source":"user"}}
{"type":"response_item","timestamp":"2026-05-25T12:00:00Z","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"我喜欢举例"}]}}`
	result, err := parseDiscovery(strings.NewReader(legacy))
	if err != nil || len(result) != 1 || result[0].Basis != "legacy_user_role_log_time_unverified" {
		t.Fatal("legacy uncertainty lost", err)
	}
}

// TestBarePasswordRedaction 遮盖未写冒号或“是”的明确密码文本，不保留凭据值。
func TestBarePasswordRedaction(t *testing.T) {
	text, _ := redactCodexText("密码Demo123456，密码是Test456789。正常句子")
	if strings.Contains(text, "Demo123456") || strings.Contains(text, "Test456789") {
		t.Fatal("bare password leaked")
	}
}

// TestAssistedGoldNotFormal 即使改名成正式样本，AI 辅助审阅也不能通过正式实验入口。
func TestAssistedGoldNotFormal(t *testing.T) {
	errors := Readiness(nil, Config{}, []Annotation{{SampleID: "U1", Method: "ai_assisted"}})
	if !strings.Contains(strings.Join(errors, " "), "独立人工金标") {
		t.Fatal("assisted gold not blocked")
	}
}
