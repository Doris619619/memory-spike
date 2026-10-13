// 文件用途：防止空输出、未完成标注或合成样本让正式质量评分虚高。
package spike

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// reviewFixture 创建测试专用快照，真实/合成标记仅用于验证统计逻辑，绝不进入正式样本。
func reviewFixture(t *testing.T, provenance string, blocks []Block) (string, string) {
	t.Helper()
	dir := t.TempDir()
	sample := Sample{ID: "U1", Provenance: provenance, Messages: []Message{{ID: "m_001", Text: "我偏好中文"}}}
	annotations := []Annotation{}
	if provenance == "real" {
		annotations = append(annotations, Annotation{SampleID: "U1", Annotator: "A", Entries: []Gold{{ID: "g1", Category: "auto"}}})
	}
	if err := WriteJSON(filepath.Join(dir, "manifest.json"), map[string]any{"samples": []Sample{sample}, "gold": annotations}); err != nil {
		t.Fatal(err)
	}
	run := Run{ID: "r1", SampleID: "U1", Provenance: provenance, State: "finished", Variant: "V1", Model: Model{ID: "test"}, Parsed: &Extraction{Blocks: blocks, Conflicts: []Conflict{}}}
	b, _ := json.Marshal(run)
	if err := WriteNew(filepath.Join(dir, "runs.jsonl"), append(b, '\n')); err != nil {
		t.Fatal(err)
	}
	reviews := filepath.Join(t.TempDir(), "reviews")
	if err := PrepareReview(dir, reviews); err != nil {
		t.Fatal(err)
	}
	return dir, reviews
}

// completeRunReview 只修改测试文件的运行确认；不替代生产流程的人工判断。
func completeRunReview(t *testing.T, reviews string) {
	t.Helper()
	path := filepath.Join(reviews, "run_reviews.csv")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(b), "\ufeff"))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	rows[1][3] = "true"
	rows[1][4] = "0"
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := csv.NewWriter(f)
	w.WriteAll(rows)
	if err := w.Error(); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

// TestUnreviewedBlocksStayPending 检查勾选完成但没填块评分时仍显示待评分。
func TestUnreviewedBlocksStayPending(t *testing.T) {
	dir, reviews := reviewFixture(t, "real", []Block{{Section: "沟通偏好", Text: "偏好中文", Evidence: []string{"m_001"}}})
	completeRunReview(t, reviews)
	out := filepath.Join(t.TempDir(), "score")
	if err := Score(dir, reviews, out); err != nil {
		t.Fatal(err)
	}
	rows, err := readCSV(filepath.Join(out, "metrics.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if rows[0]["review_complete"] != "false" || rows[0]["precision"] != "待评分" {
		t.Fatal("blank scores accepted")
	}
}

// TestEmptyExtractionRecallIsZero 真实样本有 auto 金标而模型返回空数组时，recall 必须是零。
func TestEmptyExtractionRecallIsZero(t *testing.T) {
	dir, reviews := reviewFixture(t, "real", []Block{})
	completeRunReview(t, reviews)
	out := filepath.Join(t.TempDir(), "score")
	if err := Score(dir, reviews, out); err != nil {
		t.Fatal(err)
	}
	rows, err := readCSV(filepath.Join(out, "metrics.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if rows[0]["recall"] != "0.0000" || rows[0]["precision"] != "N/A" {
		t.Fatal("empty output inflated quality")
	}
	report, _ := os.ReadFile(filepath.Join(out, "summary.md"))
	if !strings.Contains(string(report), "0.000 / 0.000 (n=1)") {
		t.Fatal("zero recall excluded from aggregate")
	}
}

// TestSyntheticExcludedAndMissingReviewRejected 确保合成样本不进入真实均值，删除评分行不能隐藏失败。
func TestSyntheticExcludedAndMissingReviewRejected(t *testing.T) {
	dir, reviews := reviewFixture(t, "synthetic", []Block{})
	completeRunReview(t, reviews)
	out := filepath.Join(t.TempDir(), "score")
	if err := Score(dir, reviews, out); err != nil {
		t.Fatal(err)
	}
	report, _ := os.ReadFile(filepath.Join(out, "summary.md"))
	if strings.Contains(string(report), "| test / V1 / rules-reviewer |") {
		t.Fatal("synthetic entered real mean")
	}
	os.WriteFile(filepath.Join(reviews, "run_reviews.csv"), []byte("run_id,sample_id,annotator,review_complete,correctly_deferred_conflicts,notes\n"), 0600)
	if err := Score(dir, reviews, filepath.Join(t.TempDir(), "rejected")); err == nil {
		t.Fatal("missing review silently omitted")
	}
}
