// 文件用途：用虚构聊天验证保存恢复、完成门槛、证据绑定及本机 HTTP 访问边界。
package annotator

import (
	"bytes"
	"encoding/json"
	"memory-spike/internal/spike"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture 只构造虚构数据，返回可重新打开的输入文件和独立进度文件。
func fixture(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	input := filepath.Join(dir, "preview.json")
	sample := spike.Sample{ID: "C1", Provenance: "real", Messages: []spike.Message{{ID: "m_001", Session: "s_01", Date: "2026-10-12T12:00:00Z", Text: "我喜欢简洁的中文回答。"}, {ID: "m_002", Session: "s_01", Date: "2026-10-12T12:01:00Z", Text: "帮我算一下 2+2。"}}, MessageCount: 2, SessionCount: 1}
	preview := spike.CodexPreview{LocalProcessingConsent: true, Sample: sample, SampleHash: spike.SampleHash(sample)}
	if err := spike.WriteJSON(input, preview); err != nil {
		t.Fatal(err)
	}
	store, err := Open(input, filepath.Join(dir, "review.json"))
	if err != nil {
		t.Fatal(err)
	}
	return store, input
}

// completeState 模拟真人明确选择，未使用真实样本或模型生成内容。
func completeState(s *Store) State {
	next := s.Snapshot()
	next.Annotation.Complete = true
	next.Marks["m_001"] = Mark{Status: "marked", Entries: []spike.Gold{{Fact: "喜欢简洁的中文回答", Category: "auto", Stability: "用户明确自述", Evidence: []string{"m_001"}}}}
	next.Marks["m_002"] = Mark{Status: "skip", Entries: []spike.Gold{}}
	return next
}

// TestSaveResumeAndReopen 验证磁盘恢复、金标同步、重复保存和重新编辑撤销完成。
func TestSaveResumeAndReopen(t *testing.T) {
	s, input := fixture(t)
	next := completeState(s)
	saved, err := s.Save(next)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Annotation.Complete || saved.Annotation.Entries[0].ID != "g1" {
		t.Fatal("gold not generated")
	}
	reopened, err := Open(input, s.Path)
	if err != nil {
		t.Fatal(err)
	}
	next = reopened.Snapshot()
	next.Annotation.Complete = false
	next.Marks["m_002"] = Mark{Status: "uncertain"}
	next.Current = 1
	if _, err = reopened.Save(next); err != nil {
		t.Fatal(err)
	}
	reopened, err = Open(input, s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Snapshot().Annotation.Complete || reopened.Snapshot().Current != 1 {
		t.Fatal("stale completion or cursor")
	}
}

// TestIncompleteAndInvalidFinal 验证未审阅、拿不准、空事实、缺依据和伪造证据不能提交完成。
func TestIncompleteAndInvalidFinal(t *testing.T) {
	for _, kind := range []string{"unseen", "uncertain", "empty", "stability", "evidence", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			s, _ := fixture(t)
			next := completeState(s)
			mark := next.Marks["m_001"]
			switch kind {
			case "unseen":
				delete(next.Marks, "m_002")
			case "uncertain":
				next.Marks["m_002"] = Mark{Status: "uncertain"}
			case "empty":
				mark.Entries[0].Fact = " "
			case "stability":
				mark.Entries[0].Stability = ""
			case "evidence":
				mark.Entries[0].Evidence = []string{"m_404"}
			case "unknown":
				next.Marks["m_404"] = Mark{Status: "skip"}
			}
			next.Marks["m_001"] = mark
			if _, err := s.Save(next); err == nil {
				t.Fatal("accepted invalid final")
			}
			if _, err := os.Stat(s.Path); !os.IsNotExist(err) {
				t.Fatal("invalid final wrote state")
			}
		})
	}
}

// TestDraftAndConflict 草稿可空缺；旧版本写入失败且不改变磁盘进度。
func TestDraftAndConflict(t *testing.T) {
	s, _ := fixture(t)
	draft := s.Snapshot()
	draft.Marks["m_001"] = Mark{Status: "marked", Entries: []spike.Gold{{Category: "auto", Evidence: []string{"m_001"}}}}
	if _, err := s.Save(draft); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(s.Path)
	if _, err := s.Save(draft); err == nil {
		t.Fatal("stale save accepted")
	}
	after, _ := os.ReadFile(s.Path)
	if !bytes.Equal(before, after) {
		t.Fatal("conflict changed disk")
	}
}

// TestInputMismatch 损坏已有进度、换样本或篡改摘要时不静默从零覆盖。
func TestInputMismatch(t *testing.T) {
	s, input := fixture(t)
	if _, err := s.Save(s.Snapshot()); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(input)
	b = bytes.ReplaceAll(b, []byte("简洁"), []byte("详细"))
	if err := os.WriteFile(input, b, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(input, s.Path); err == nil {
		t.Fatal("tampered sample accepted")
	}
}

// TestHTTPBoundary 验证跨站、错误 Host、无令牌、未知路由被拒绝，合法保存可恢复。
func TestHTTPBoundary(t *testing.T) {
	s, _ := fixture(t)
	host := "127.0.0.1:8787"
	handler := Handler(s, host)
	request := httptest.NewRequest("GET", "http://"+host+"/api/state", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var boot struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &boot); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(s.Snapshot())
	for _, kind := range []string{"valid", "origin", "host", "token", "trailing"} {
		t.Run(kind, func(t *testing.T) {
			payload := string(body)
			if kind == "trailing" {
				payload += " {}"
			}
			r := httptest.NewRequest("POST", "http://"+host+"/api/state", strings.NewReader(payload))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Annotation-Token", boot.Token)
			switch kind {
			case "origin":
				r.Header.Set("Origin", "https://example.com")
			case "host":
				r.Host = "attacker.test:8787"
			case "token":
				r.Header.Del("X-Annotation-Token")
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if (kind == "valid") != (w.Code == 200) {
				t.Fatalf("%s: %d", kind, w.Code)
			}
		})
	}
	r := httptest.NewRequest("GET", "http://"+host+"/review.json", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal("private path exposed")
	}
}
