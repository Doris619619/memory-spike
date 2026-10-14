// 文件用途：用虚构数据验证范围保存恢复、旧标注兼容及不完整项目不能确认。
package annotator

import "testing"

// TestScopeRoundTrip 项目范围与长期类别同时存在，保存恢复和导出都必须保留项目名。
func TestScopeRoundTrip(t *testing.T) {
	s, input := fixture(t)
	next := completeState(s)
	mark := next.Marks["m_001"]
	mark.Entries[0].Scope = "project"
	mark.Entries[0].Project = "虚构课程项目"
	next.Marks["m_001"] = mark
	if _, err := s.Save(next); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(input, s.Path)
	if err != nil {
		t.Fatal(err)
	}
	state := reopened.Snapshot()
	for _, g := range append(state.Annotation.Entries, state.Marks["m_001"].Entries...) {
		if g.Scope != "project" || g.Project != "虚构课程项目" || g.Category != "auto" {
			t.Fatal("scope or category lost")
		}
	}
}

// TestScopeValidation 允许旧数据未知范围和未完成草稿，拒绝非法范围或带错范围的项目名。
func TestScopeValidation(t *testing.T) {
	for _, tc := range []struct {
		name, scope, project      string
		complete, reviewed, valid bool
	}{
		{"legacy", "", "", true, false, true},
		{"global", "global", "", true, false, true},
		{"conversation", "conversation", "", true, false, true},
		{"project-draft", "project", "", false, false, true},
		{"project-complete", "project", " ", true, false, false},
		{"project-reviewed", "project", "", false, true, false},
		{"invalid-scope", "all", "", false, false, false},
		{"orphan-project", "global", "虚构项目", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := fixture(t)
			next := completeState(s)
			next.Annotation.Complete = tc.complete
			mark := next.Marks["m_001"]
			mark.Reviewed = tc.reviewed
			mark.Entries[0].Scope, mark.Entries[0].Project = tc.scope, tc.project
			next.Marks["m_001"] = mark
			_, err := s.Save(next)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, got %v", tc.valid, err)
			}
		})
	}
}
