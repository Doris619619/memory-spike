//go:build ignore

// 文件用途：创建人工评分表，或读取已填写的评分表生成真实与合成分离的统计。
package main

import (
	"flag"
	"fmt"
	"memory-spike/internal/spike"
	"os"
)

// main 默认只准备空表；只有显式 -score 才读取人工判断并计算质量指标。
func main() {
	dir := flag.String("run-dir", "", "运行目录")
	out := flag.String("out", "", "仓库外新输出目录")
	score := flag.Bool("score", false, "汇总人工评分")
	reviews := flag.String("reviews", "", "已填 CSV 所在目录")
	flag.Parse()
	if *dir == "" || *out == "" {
		fail(fmt.Errorf("请指定 -run-dir 和 -out"))
	}
	var err error
	if *score {
		if *reviews == "" {
			fail(fmt.Errorf("请指定 -reviews"))
		}
		err = spike.Score(*dir, *reviews, *out)
	} else {
		err = spike.PrepareReview(*dir, *out)
	}
	if err != nil {
		fail(err)
	}
	fmt.Println("已生成：" + *out)
}

// fail 不自动覆盖或修正人工评分文件。
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
