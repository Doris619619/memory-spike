// 文件用途：生成块级人工评分表，并把运行结果、用量和证据原文放在仓库外供评审。
package spike

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// LatestRuns 合并 started/finished 两条事件；只有 started 的组合保留为结果未知，不算成功。
func LatestRuns(path string) ([]Run, error) {
	events, err := ReadJSONL[Run](path)
	if err != nil {
		return nil, err
	}
	latest := map[string]Run{}
	order := []string{}
	for _, r := range events {
		if _, ok := latest[r.ID]; !ok {
			order = append(order, r.ID)
		}
		latest[r.ID] = r
	}
	runs := []Run{}
	for _, id := range order {
		runs = append(runs, latest[id])
	}
	return runs, nil
}

// writeCSV 写 UTF-8 BOM 供 Windows Excel 打开，危险公式前缀加单引号，避免将模型文本当公式执行。
func writeCSV(path string, records [][]string) error {
	var b bytes.Buffer
	b.WriteString("\xef\xbb\xbf")
	w := csv.NewWriter(&b)
	for _, record := range records {
		safe := make([]string, len(record))
		for i, s := range record {
			if strings.ContainsAny(strings.TrimLeft(s, " \t\r\n")[:min(1, len(strings.TrimLeft(s, " \t\r\n")))], "=+-@") {
				safe[i] = "'" + s
			} else {
				safe[i] = s
			}
		}
		if err := w.Write(safe); err != nil {
			return err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	return WriteNew(path, b.Bytes())
}

// LoadManifest 读取本次冻结的取样、金标与配置，不从当前可变文件重算实验身份。
func LoadManifest(dir string) ([]Sample, []Annotation, error) {
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, nil, err
	}
	var m struct {
		Samples []Sample     `json:"samples"`
		Gold    []Annotation `json:"gold"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, nil, err
	}
	return m.Samples, m.Gold, nil
}

// PrepareReview 为每位金标标注者生成独立评分行；E2 可机械判断，其余语义字段保持空白。
func PrepareReview(dir, out string) error {
	runs, err := LatestRuns(filepath.Join(dir, "runs.jsonl"))
	if err != nil {
		return err
	}
	samples, gold, err := LoadManifest(dir)
	if err != nil {
		return err
	}
	bySample := map[string]Sample{}
	for _, s := range samples {
		bySample[s.ID] = s
	}
	blocks := [][]string{{"run_id", "sample_id", "annotator", "block_index", "block_text", "evidence_ids", "E1", "E2", "F1", "S1", "X1", "M1", "matched_auto_gold_ids", "notes"}}
	checks := [][]string{{"run_id", "sample_id", "annotator", "review_complete", "correctly_deferred_conflicts", "notes"}}
	var report strings.Builder
	report.WriteString("<!-- 文件用途：展示真实实验输出和证据，供人工逐条评分；保存在仓库之外。 -->\n# 抽取实验待评审结果\n\n本文件不是 Go 结论。E1/F1/S1/X1/M1 与 recall 等待人工填写。合成样本不进入真实质量统计。\n\n")
	for _, r := range runs {
		fmt.Fprintf(&report, "## %s · %s · %s\n\n状态：%s；HTTP：%d；格式失败：%s；延迟：%d ms。\n\n", r.SampleID, r.Model.ID, r.Variant, r.State, r.HTTPStatus, r.FormatFailure, r.LatencyMS)
		if r.State == "unavailable" || r.State == "started" {
			continue
		}
		annotators := []string{}
		for _, g := range gold {
			if g.SampleID == r.SampleID {
				annotators = append(annotators, g.Annotator)
			}
		}
		if len(annotators) == 0 {
			annotators = []string{"rules-reviewer"}
		}
		s := bySample[r.SampleID]
		messages := map[string]string{}
		for _, m := range s.Messages {
			messages[m.ID] = m.Text
		}
		for _, a := range annotators {
			checks = append(checks, []string{r.ID, r.SampleID, a, "", "", ""})
		}
		if r.Parsed == nil {
			report.WriteString("没有可评分的解析结果；请检查 runs.jsonl 中的原始输出。\n\n")
			continue
		}
		for i, b := range r.Parsed.Blocks {
			fmt.Fprintf(&report, "### 候选 %d\n\n%s\n\n", i, b.Text)
			fabricated := false
			for _, e := range b.Evidence {
				text, ok := messages[e]
				if !ok {
					fabricated = true
					text = "[不存在的消息 ID]"
				}
				fmt.Fprintf(&report, "- %s：%s\n", e, strings.ReplaceAll(text, "\n", " "))
			}
			report.WriteString("\n")
			for _, a := range annotators {
				blocks = append(blocks, []string{r.ID, r.SampleID, a, strconv.Itoa(i), b.Text, strings.Join(b.Evidence, ";"), "", strconv.FormatBool(fabricated), "", "", "", "", "", ""})
			}
		}
		if len(r.Parsed.Conflicts) > 0 {
			b, _ := json.MarshalIndent(r.Parsed.Conflicts, "", "  ")
			fmt.Fprintf(&report, "冲突候选（正确延迟数量仍需人工判定）：\n\n```json\n%s\n```\n\n", b)
		}
	}
	if err := writeCSV(filepath.Join(out, "blocks.csv"), blocks); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(out, "run_reviews.csv"), checks); err != nil {
		return err
	}
	return WriteNew(filepath.Join(out, "review.md"), []byte(report.String()))
}

// readCSV 按列名读取人工表；不把空值解释成通过。
func readCSV(path string) ([]map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(b), "\ufeff")))
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("评分表为空")
	}
	result := []map[string]string{}
	for _, row := range rows[1:] {
		m := map[string]string{}
		for i, h := range rows[0] {
			m[h] = row[i]
		}
		result = append(result, m)
	}
	return result, nil
}

// Score 对照冻结输出验证每个评分行，计算各标注者独立指标，不擅自合并 U2/U6 分歧。
func Score(dir, reviews, out string) error {
	runs, err := LatestRuns(filepath.Join(dir, "runs.jsonl"))
	if err != nil {
		return err
	}
	samples, gold, err := LoadManifest(dir)
	if err != nil {
		return err
	}
	rows, err := readCSV(filepath.Join(reviews, "blocks.csv"))
	if err != nil {
		return err
	}
	checks, err := readCSV(filepath.Join(reviews, "run_reviews.csv"))
	if err != nil {
		return err
	}
	byID := map[string]Run{}
	sampleMap := map[string]Sample{}
	for _, r := range runs {
		byID[r.ID] = r
	}
	for _, s := range samples {
		sampleMap[s.ID] = s
	}
	results := [][]string{{"run_id", "sample_id", "model", "variant", "annotator", "provenance", "review_complete", "block_count", "precision", "E1_rate", "E2_rate", "recall", "correctly_deferred_conflicts", "format_failure", "input_tokens", "output_tokens", "latency_ms", "estimated_cost", "currency"}}
	var report strings.Builder
	report.WriteString("<!-- 文件用途：汇总人工评分和机器指标；不替代团队的 Go/No-Go 评审。 -->\n# SPIKE 评分汇总\n\n六样本只用于发现系统性错误。合成样本不计入真实质量统计；U2/U6 保留独立标注结果，分歧需人工对照金标裁决。费用是按配置单价估算，不是账单结算值。\n\n")
	aggregates := map[string][][3]float64{}
	pending := 0
	seenChecks := map[string]bool{}
	for _, row := range rows {
		if _, ok := byID[row["run_id"]]; !ok {
			return fmt.Errorf("块评分引用未知运行")
		}
	}
	for _, check := range checks {
		r, ok := byID[check["run_id"]]
		if !ok {
			return fmt.Errorf("评分引用不存在的 run_id")
		}
		a := check["annotator"]
		key := r.ID + "|" + a
		if seenChecks[key] {
			return fmt.Errorf("运行评分重复")
		}
		seenChecks[key] = true
		s := sampleMap[r.SampleID]
		auto := map[string]bool{}
		validAnnotator := s.Provenance == "synthetic" && a == "rules-reviewer"
		for _, g := range gold {
			if g.SampleID == r.SampleID && g.Annotator == a {
				validAnnotator = true
				for _, e := range g.Entries {
					if e.Category == "auto" {
						auto[e.ID] = true
					}
				}
			}
		}
		if !validAnnotator {
			return fmt.Errorf("标注者不在冻结金标中")
		}
		count := 0
		if r.Parsed != nil {
			count = len(r.Parsed.Blocks)
		}
		complete := check["review_complete"] == "true"
		matched := map[string]bool{}
		seenBlocks := map[int]bool{}
		good, evidence, fabricated := 0, 0, 0
		for _, row := range rows {
			if row["run_id"] != r.ID || row["annotator"] != a {
				continue
			}
			idx, err := strconv.Atoi(row["block_index"])
			if err != nil || idx < 0 || idx >= count || seenBlocks[idx] {
				return fmt.Errorf("块评分索引不合法或重复")
			}
			seenBlocks[idx] = true
			valid := true
			for _, f := range []string{"E1", "E2", "S1", "X1", "M1"} {
				if row[f] != "true" && row[f] != "false" {
					valid = false
				}
			}
			if row["F1"] != "正确" && row["F1"] != "过度泛化" && row["F1"] != "错误" {
				valid = false
			}
			if !valid {
				complete = false
				continue
			}
			known := map[string]bool{}
			for _, m := range s.Messages {
				known[m.ID] = true
			}
			actualE2 := false
			for _, id := range r.Parsed.Blocks[idx].Evidence {
				if !known[id] {
					actualE2 = true
				}
			}
			if row["E2"] != strconv.FormatBool(actualE2) {
				return fmt.Errorf("E2 必须与实际证据 ID 校验一致")
			}
			if actualE2 && row["E1"] == "true" {
				return fmt.Errorf("伪造证据不能标为 E1 有效")
			}
			if row["E1"] == "true" {
				evidence++
			}
			if row["E2"] == "true" {
				fabricated++
			}
			if row["E1"] == "true" && row["E2"] == "false" && row["F1"] == "正确" && row["S1"] == "true" && row["X1"] == "false" && row["M1"] == "false" {
				good++
			}
			for _, id := range strings.Split(row["matched_auto_gold_ids"], ";") {
				id = strings.TrimSpace(id)
				if id == "" {
					continue
				}
				if !auto[id] {
					return fmt.Errorf("recall 只能匹配该标注者的 auto 金标 ID")
				}
				if row["F1"] != "正确" || row["E1"] != "true" {
					return fmt.Errorf("错误或无证据的块不能计入金标命中")
				}
				matched[id] = true
			}
		}
		if len(seenBlocks) != count {
			complete = false
		}
		deferred, parseErr := strconv.Atoi(check["correctly_deferred_conflicts"])
		maxConflicts := 0
		if r.Parsed != nil {
			maxConflicts = len(r.Parsed.Conflicts)
		}
		if parseErr != nil || deferred < 0 || deferred > maxConflicts {
			complete = false
		}
		precision, e1, e2, recall := "待评分", "待评分", "待评分", "待评分"
		if complete {
			precision = ratio(good, count)
			e1 = ratio(evidence, count)
			e2 = ratio(fabricated, count)
			recall = ratio(len(matched), len(auto))
			if s.Provenance == "real" {
				group := r.Model.ID + " / " + r.Variant + " / " + a
				v := [3]float64{-1, -1, -1}
				if count > 0 {
					v[0] = float64(good) / float64(count)
					v[1] = float64(evidence) / float64(count)
				}
				if len(auto) > 0 {
					v[2] = float64(len(matched)) / float64(len(auto))
				}
				aggregates[group] = append(aggregates[group], v)
			}
		} else {
			pending++
		}
		results = append(results, []string{r.ID, r.SampleID, r.Model.ID, r.Variant, a, s.Provenance, strconv.FormatBool(complete), strconv.Itoa(count), precision, e1, e2, recall, check["correctly_deferred_conflicts"], r.FormatFailure, intText(r.InputTokens), intText(r.OutputTokens), strconv.FormatInt(r.LatencyMS, 10), costText(r.EstimatedCost), r.Model.Currency})
	}
	for _, r := range runs {
		if r.State == "started" || r.State == "unavailable" {
			fmt.Fprintf(&report, "- %s / %s / %s：%s，未算作完成的生成。\n", r.SampleID, r.Model.ID, r.Variant, r.State)
			continue
		}
		expected := []string{}
		for _, g := range gold {
			if g.SampleID == r.SampleID {
				expected = append(expected, g.Annotator)
			}
		}
		if len(expected) == 0 {
			expected = []string{"rules-reviewer"}
		}
		for _, a := range expected {
			if !seenChecks[r.ID+"|"+a] {
				return fmt.Errorf("缺失运行评分行；请恢复完整 run_reviews.csv")
			}
		}
	}
	for _, row := range rows {
		if !seenChecks[row["run_id"]+"|"+row["annotator"]] {
			return fmt.Errorf("存在无对应运行评分的块评分行")
		}
	}
	fmt.Fprintf(&report, "\n尚有 %d 组人工评分未完成。下列均值仅包含已完成的真实样本；不能据此自动批准上线。\n\n| 模型 / 变体 / 标注者 | 样本数 | 精确率均值 / 最差 | E1 均值 / 最差 | recall 均值 / 最差 |\n| --- | --- | --- | --- | --- |\n", pending)
	keys := []string{}
	for k := range aggregates {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		values := aggregates[k]
		sum, low, counts := [3]float64{}, [3]float64{1, 1, 1}, [3]int{}
		for _, v := range values {
			for i := 0; i < 3; i++ {
				if v[i] < 0 {
					continue
				}
				counts[i]++
				sum[i] += v[i]
				low[i] = min(low[i], v[i])
			}
		}
		cells := [3]string{}
		for i := 0; i < 3; i++ {
			cells[i] = "N/A"
			if counts[i] > 0 {
				cells[i] = fmt.Sprintf("%.3f / %.3f (n=%d)", sum[i]/float64(counts[i]), low[i], counts[i])
			}
		}
		fmt.Fprintf(&report, "| %s | %d | %s | %s | %s |\n", k, len(values), cells[0], cells[1], cells[2])
	}
	report.WriteString(machineSummary(runs))
	report.WriteString("\n方案方向性标准：精确率 ≥0.80，E1 ≥0.90，recall ≥0.60；敏感违规、格式失败率和两轮 prompt 结果需要结合方案 §10 人工评审。零分母记为 N/A，不能作 100%。\n")
	if err := writeCSV(filepath.Join(out, "metrics.csv"), results); err != nil {
		return err
	}
	return WriteNew(filepath.Join(out, "summary.md"), []byte(report.String()))
}

// machineSummary 将全部请求的格式/网络失败保留在分母中，V0 unavailable 与执行未知单列。
func machineSummary(runs []Run) string {
	type counters struct{ total, nonJSON, schema, empty, truncated, failed, unknown, unavailable int }
	groups := map[string]*counters{}
	keys := []string{}
	for _, r := range runs {
		k := r.Model.ID + " / " + r.Variant + " / " + r.Provenance
		c := groups[k]
		if c == nil {
			c = &counters{}
			groups[k] = c
			keys = append(keys, k)
		}
		if r.State == "unavailable" {
			c.unavailable++
			continue
		}
		if r.State == "started" {
			c.unknown++
			continue
		}
		c.total++
		if r.State != "finished" {
			c.failed++
		}
		switch r.FormatFailure {
		case "non_json":
			c.nonJSON++
		case "schema_mismatch":
			c.schema++
		case "empty_output":
			c.empty++
		case "truncated":
			c.truncated++
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("\n## 机器结果（真实与合成分开）\n\n| 模型 / 变体 / 来源 | 已有结果 | 非 JSON | schema 不符 | 空输出 | 截断 | 网络/协议失败 | 执行未知 | V0 不可用 | 格式失败率 |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, k := range keys {
		c := groups[k]
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d | %d | %d | %d | %s |\n", k, c.total, c.nonJSON, c.schema, c.empty, c.truncated, c.failed, c.unknown, c.unavailable, ratio(c.nonJSON+c.schema+c.empty+c.truncated, c.total))
	}
	return b.String()
}

// ratio 对无候选或无 auto 金标返回 N/A，避免空样本被误算为满分。
func ratio(a, b int) string {
	if b == 0 {
		return "N/A"
	}
	return fmt.Sprintf("%.4f", float64(a)/float64(b))
}

// intText 区分接口未给用量和真实零用量。
func intText(value *int) string {
	if value == nil {
		return "unknown"
	}
	return strconv.Itoa(*value)
}

// costText 单价或 usage 缺失时不编造费用。
func costText(value *float64) string {
	if value == nil {
		return "unknown"
	}
	return strconv.FormatFloat(*value, 'f', 8, 64)
}
