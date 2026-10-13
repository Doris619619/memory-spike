//go:build ignore

// 文件用途：运行正式六样本 V0/V1 矩阵，或只检查样本、金标与价格是否齐全。
package main

import (
	"flag"
	"fmt"
	"memory-spike/internal/spike"
	"os"
)

// main 在 -check 模式只检查准备情况，不读取 Key、不调用模型。
func main() {
	samples := flag.String("samples", "", "仓库外 samples.jsonl")
	config := flag.String("config", "../../configs/models.example.json", "模型配置")
	gold := flag.String("gold-dir", "", "仓库外人工金标目录")
	promptPath := flag.String("prompt", "../../prompts/v0.txt", "冻结的提示词")
	out := flag.String("out", "", "仓库外独立运行目录")
	check := flag.Bool("check", false, "只检查前置条件")
	flag.Parse()
	if *samples == "" || *gold == "" {
		fail(fmt.Errorf("请指定 -samples 和 -gold-dir"))
	}
	s, c, a, err := spike.LoadInputs(*samples, *config, *gold)
	if err != nil {
		fail(err)
	}
	prompt, err := os.ReadFile(*promptPath)
	if err != nil {
		fail(err)
	}
	issues := spike.Readiness(s, c, a)
	if len(issues) > 0 {
		for _, issue := range issues {
			fmt.Println("待准备：" + issue)
		}
		os.Exit(2)
	}
	if *check {
		fmt.Printf("已就绪：%d 样本 × %d 模型 × %d 变体；本次没有调用 API。\n", len(s), len(c.Models), len(c.Variants))
		return
	}
	if *out == "" {
		fail(fmt.Errorf("请指定仓库外 -out 目录"))
	}
	if err := spike.Execute(s, c, a, string(prompt), *out); err != nil {
		fail(err)
	}
}

// fail 只显示受控错误消息，原始模型响应保存在仓库外日志中。
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
