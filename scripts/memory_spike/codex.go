//go:build ignore

// 文件用途：将本人授权的 Codex 本地记录整理为待核对预实验样本，完全不联网。
package main

import (
	"flag"
	"fmt"
	"memory-spike/internal/spike"
	"os"
	"strings"
	"time"
)

// main 只读显式指定的记录目录；local-consent 仅授权本地处理，不等同于模型传输同意。
func main() {
	roots := flag.String("roots", "", "Codex 记录目录，多个用分号分隔")
	out := flag.String("out", "", "仓库外、尚不存在的输出目录")
	now := flag.String("now", time.Now().UTC().Format(time.RFC3339), "固定结束时刻 RFC3339")
	consent := flag.Bool("local-consent", false, "本人已同意本地读取和处理记录")
	flag.Parse()
	if *roots == "" || *out == "" || !*consent {
		fail(fmt.Errorf("需要 -roots、-out 和 -local-consent"))
	}
	if err := spike.PrivatePath(*out); err != nil {
		fail(err)
	}
	if _, err := os.Stat(*out); !os.IsNotExist(err) {
		fail(fmt.Errorf("输出目录已存在或不可访问"))
	}
	end, err := time.Parse(time.RFC3339, *now)
	if err != nil {
		fail(fmt.Errorf("now 必须为 RFC3339"))
	}
	paths, err := spike.FindCodexFiles(strings.Split(*roots, ";"), end.AddDate(0, 0, -30))
	if err != nil {
		fail(err)
	}
	fmt.Printf("检查 %d 个近期记录文件；仅在本地处理，不输出原文。\n", len(paths))
	preview, err := spike.BuildCodexPreview(paths, end, true)
	if err != nil {
		fail(err)
	}
	if err := spike.WriteCodexPreview(preview, *out); err != nil {
		fail(err)
	}
	fmt.Printf("已生成本人预实验样本：%d 会话，%d 条消息，约 %.0f token；没有调用模型。\n", preview.Sample.SessionCount, preview.Sample.MessageCount, preview.Sample.ApproxTokens)
	fmt.Println("核对文件：" + *out + "/sample-review.md")
}

// fail 仅输出受控结构错误，不泄露原始消息、路径索引或凭据。
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
