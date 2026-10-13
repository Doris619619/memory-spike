// 文件用途：验证取样范围、失败记录、证据校验及数据保护等会改变实验结论的边界。
package spike

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testSession 构造合成会话，仅用于单元测试，绝不将其当作真实实验样本。
func testSession(id string, n int, now time.Time) Session {
	s := Session{ID: id, Source: "web", Updated: now, Layout: fmt.Sprintf(`{"displayed_msg_idx":%d}`, n-1)}
	for i := 0; i < n; i++ {
		b, _ := json.Marshal(map[string]any{"content": "我偏好先看例子。", "created_at": now.Add(-time.Duration(n-i) * time.Minute).UnixMilli()})
		s.Rows = append(s.Rows, Row{Index: i, Parent: i - 1, Role: "user", Content: string(b)})
	}
	return s
}

// TestCollectFilters 验证渠道、隐藏分支、合成消息、旧 campus 和 content.created_at 日期过滤。
func TestCollectFilters(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	s := testSession("ordinary", 4, now)
	s.Rows[1].Content = `{"content":"<agent-message>排除</agent-message>","created_at":1791892800000}`
	s.Rows[2].Content = `{"content":"过期消息","created_at":1000}`
	s.Rows = append(s.Rows, Row{Index: 99, Parent: 0, Role: "user", Content: `{"content":"隐藏分支","created_at":1791892800000}`})
	wechat := testSession("wechat", 1, now)
	wechat.Source = "wechat"
	project := testSession("p", 1, now)
	project.Project = true
	child := testSession("c", 1, now)
	child.Child = true
	campus := testSession("campus", 2, now)
	campus.Rows[1].Role = "approach"
	campus.Rows[1].Approach = "campus-affairs-qa"
	result, err := Collect([]Session{s, wechat, project, child, campus}, "U4", "synthetic", false, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.MessageCount != 2 || result.SessionCount != 1 {
		t.Fatalf("wrong scope: %#v", result)
	}
}

// TestCollectRejectsBrokenBranch 防止错误布局时偷用最新消息或漏掉环检查。
func TestCollectRejectsBrokenBranch(t *testing.T) {
	now := time.Now().UTC()
	s := testSession("one", 2, now)
	s.Rows[0].Parent = 1
	if _, err := Collect([]Session{s}, "U4", "synthetic", false, now); err == nil {
		t.Fatal("cycle accepted")
	}
	s.Rows[0].Parent = -1
	s.Layout = `{}`
	if _, err := Collect([]Session{s}, "U4", "synthetic", false, now); err == nil {
		t.Fatal("missing leaf accepted")
	}
}

// TestCollectCapsAndDeterminism 覆盖 50 会话、500 消息、每会话先取 15 条及稳定摘要。
func TestCollectCapsAndDeterminism(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	sessions := []Session{}
	for i := 0; i < 55; i++ {
		sessions = append(sessions, testSession(fmt.Sprint(i), 30, now.Add(-time.Duration(i)*time.Hour)))
	}
	s, err := Collect(sessions, "U3", "real", true, now)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Sampled || s.MessageCount != 500 || s.SessionCount > 50 || s.EligibleMessages != 1650 {
		t.Fatalf("wrong caps: %#v", s)
	}
	if issues := ValidateSample(s); len(issues) > 0 {
		t.Fatal(issues)
	}
	again, _ := Collect(sessions, "U3", "real", true, now)
	if SampleHash(s) != SampleHash(again) {
		t.Fatal("non-deterministic")
	}
}

// TestCollectUnicodeBudget 检查按字符而非字节截断，并遵守约 24k token 总预算。
func TestCollectUnicodeBudget(t *testing.T) {
	now := time.Now().UTC()
	s := testSession("long", 40, now)
	for i := range s.Rows {
		b, _ := json.Marshal(map[string]any{"content": strings.Repeat("中", 2100), "created_at": now.UnixMilli()})
		s.Rows[i].Content = string(b)
	}
	out, err := Collect([]Session{s}, "U5", "synthetic", false, now)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Sampled || out.ApproxTokens != 24000 || out.MessageCount != 24 || len([]rune(out.Messages[0].Text)) != 2000 {
		t.Fatal("budget/truncation failed")
	}
}

// TestRealConsentAndMatrix 阻止用合成数据替代真实样本、缺少 consent 或缺少双人标注。
func TestRealConsentAndMatrix(t *testing.T) {
	now := time.Now().UTC()
	if _, err := Collect(nil, "U1", "real", false, now); err == nil {
		t.Fatal("missing consent accepted")
	}
	s, _ := Collect([]Session{testSession("s", 1, now)}, "U6", "synthetic", false, now)
	if len(ValidateSample(s)) == 0 {
		t.Fatal("synthetic U6 accepted")
	}
	s.Provenance = "real"
	s.Consented = true
	a := Annotation{SampleID: s.ID, SampleHash: SampleHash(s), Annotator: "person-a", Complete: true, Entries: []Gold{}}
	if len(ValidateAnnotations(s, []Annotation{a})) == 0 {
		t.Fatal("U6 accepted one annotator")
	}
	b := a
	b.Annotator = "person-b"
	if len(ValidateAnnotations(s, []Annotation{a, b})) != 0 {
		t.Fatal("two independent annotations rejected")
	}
}

// TestParseVariants 区分格式错误、V1 外层包装和伪造证据；ID 存在不代表事实有证据。
func TestParseVariants(t *testing.T) {
	s := Sample{Messages: []Message{{ID: "m_001"}}}
	body := `{"blocks":[{"section":"沟通偏好","text":"偏好中文","evidence_ids":["m_999"]}],"conflicts":[]}`
	parsed, failure, issues := ParseExtraction("```json\n"+body+"\n```", "V1", s)
	if parsed == nil || failure != "" || len(issues) != 1 {
		t.Fatal("V1/evidence validation failed")
	}
	if _, failure, _ := ParseExtraction("```json\n"+body+"\n```", "V0", s); failure != "non_json" {
		t.Fatal("V0 accepted fenced text")
	}
	for _, body := range []string{`{"blocks":null,"conflicts":[]}`, `{"blocks":[],"conflicts":[],"id":"extra"}`} {
		if _, failure, _ := ParseExtraction(body, "V0", s); failure != "schema_mismatch" {
			t.Fatal("invalid schema accepted")
		}
	}
}

// TestHTTPNoRetryAndRedaction 失败响应只调用一次，原始响应中的凭据被替换。
func TestHTTPNoRetryAndRedaction(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(500)
		fmt.Fprint(w, `{"error":"test-secret"}`)
	}))
	defer server.Close()
	c := Config{BaseURL: server.URL, MaxOutputTokens: 10}
	m := Model{ID: "test", APIType: "chat.completions"}
	r := Call(NewHTTPClient(2), c, m, "V1", "prompt", "test-secret", Sample{})
	if calls != 1 || r.State != "http_error" || strings.Contains(string(r.Raw), "test-secret") {
		t.Fatal("retry or credential leak")
	}
}

// TestHTTPNoRedirect 防止 Bearer Key 跟随 aTrust 或其它重定向发送到第二个地址。
func TestHTTPNoRedirect(t *testing.T) {
	secondCalls := 0
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { secondCalls++ }))
	defer second.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, second.URL, 302) }))
	defer first.Close()
	r := Call(NewHTTPClient(2), Config{BaseURL: first.URL}, Model{APIType: "chat.completions"}, "V1", "p", "secret", Sample{})
	if secondCalls != 0 || r.HTTPStatus != 302 {
		t.Fatal("redirect followed")
	}
}

// TestV0AndUsage 检查实际发送结构化约束、解析空 blocks 为合法输出及缺失成本不冒充零元。
func TestV0AndUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&payload)
		if _, ok := payload["response_format"]; !ok {
			t.Error("no schema sent")
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"blocks\":[],\"conflicts\":[]}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":20}}`)
	}))
	defer server.Close()
	r := Call(NewHTTPClient(2), Config{BaseURL: server.URL}, Model{APIType: "chat.completions", Structured: "json_schema"}, "V0", "p", "key", Sample{})
	if r.Parsed == nil || r.FormatFailure != "" || r.InputTokens == nil || *r.InputTokens != 10 || r.EstimatedCost != nil {
		t.Fatal("wrong parse/usage")
	}
}

// TestJournalUnknownNotLost 保留只有 started 的组合，不能把中断任务当没执行过。
func TestJournalUnknownNotLost(t *testing.T) {
	p := filepath.Join(t.TempDir(), "runs.jsonl")
	f, _ := os.Create(p)
	AppendJSONL(f, Run{ID: "one", State: "started"})
	AppendJSONL(f, Run{ID: "two", State: "started"})
	AppendJSONL(f, Run{ID: "two", State: "finished"})
	f.Close()
	runs, err := LatestRuns(p)
	if err != nil || len(runs) != 2 || runs[0].State != "started" || runs[1].State != "finished" {
		t.Fatal("journal lost attempts")
	}
}

// TestPrivatePathAndNoOverwrite 阻止数据落入 Git 和覆盖既有样本。
func TestPrivatePathAndNoOverwrite(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, ".git"), 0700)
	if err := WriteNew(filepath.Join(dir, "private", "sample.json"), []byte("x")); err == nil {
		t.Fatal("accepted git path")
	}
	p := filepath.Join(t.TempDir(), "data.json")
	if err := WriteNew(p, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := WriteNew(p, []byte("two")); err == nil {
		t.Fatal("overwrote")
	}
}

// TestPriceAndCapabilityPending 保证未确认的实际单价或 schema 能力不会进入正式跑批。
func TestPriceAndCapabilityPending(t *testing.T) {
	c := Config{BaseURL: "https://example.test/open/v1", PromptVersion: "v0", MaxOutputTokens: 1500, TimeoutSeconds: 30, Variants: []string{"V0", "V1"}, Models: []Model{{ID: "a", Class: "local", Structured: "unknown"}, {ID: "b", Class: "commercial", Structured: "unknown"}}}
	if len(ValidateConfig(c)) == 0 {
		t.Fatal("unverified config accepted")
	}
	if err := Execute(nil, c, nil, "prompt", t.TempDir()); err == nil {
		t.Fatal("incomplete matrix executed")
	}
}
