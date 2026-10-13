//go:build ignore

// 文件用途：汇合匿名样本并创建人工金标空表；不生成参考答案、不调用模型。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"memory-spike/internal/spike"
	"os"
	"path/filepath"
)

// main 接收多个 collect 产出的文件；允许准备部分样本，但正式 run 仍要求完整矩阵。
func main() {
	dir := flag.String("samples-dir", "", "每个 U*.jsonl 文件所在的仓库外目录")
	out := flag.String("out", "", "合并样本输出文件")
	gold := flag.String("gold-dir", "", "人工金标空表输出目录")
	flag.Parse()
	if *dir == "" || *out == "" || *gold == "" {
		fail(fmt.Errorf("必须指定 -samples-dir、-out、-gold-dir"))
	}
	paths, err := filepath.Glob(filepath.Join(*dir, "U*.jsonl"))
	if err != nil {
		fail(err)
	}
	var all []spike.Sample
	seen := map[string]bool{}
	for _, p := range paths {
		samples, err := spike.ReadJSONL[spike.Sample](p)
		if err != nil {
			fail(err)
		}
		for _, s := range samples {
			if seen[s.ID] {
				fail(fmt.Errorf("样本 %s 重复", s.ID))
			}
			seen[s.ID] = true
			if issues := spike.ValidateSample(s); len(issues) > 0 {
				fail(fmt.Errorf("%s 不合格: %v", s.ID, issues))
			}
			all = append(all, s)
		}
	}
	if len(all) == 0 {
		fail(fmt.Errorf("没有可用的 U*.jsonl；请先 collect 或创建 U4/U5 合成夹具"))
	}
	var data []byte
	for _, s := range all {
		b, _ := json.Marshal(s)
		data = append(data, b...)
		data = append(data, '\n')
	}
	if err := spike.PrepareGold(all, *gold); err != nil {
		fail(err)
	}
	if err := spike.WriteNew(*out, data); err != nil {
		fail(err)
	}
	fmt.Printf("准备 %d 个样本；金标空表已生成，请人工独立填写。\n", len(all))
}

// fail 保留已有文件供检查，拒绝静默覆盖人工标注。
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
