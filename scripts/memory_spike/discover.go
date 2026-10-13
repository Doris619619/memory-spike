//go:build ignore

// 文件用途：经本人授权只读扫描更广的历史聊天，输出本地探索候选，不联网或生成金标。
package main

import (
	"flag"
	"fmt"
	"memory-spike/internal/spike"
	"os"
	"strings"
	"time"
)

// main 明确输入目录与私人输出文件；拒绝覆盖现有产物。
func main() {
	roots := flag.String("roots", "", "分号分隔的已授权日志目录")
	out := flag.String("out", "", "私人候选JSON")
	consent := flag.Bool("local-consent", false, "本人授权本地发现")
	flag.Parse()
	if *roots == "" || *out == "" || !*consent {
		fmt.Fprintln(os.Stderr, "需要 roots/out/local-consent")
		os.Exit(1)
	}
	paths, err := spike.FindCodexFiles(strings.Split(*roots, ";"), time.Time{})
	if err != nil {
		panic(err)
	}
	result := spike.DiscoverCodex(paths)
	if err = spike.WriteJSON(*out, result); err != nil {
		panic(err)
	}
	fmt.Printf("发现 %d 条候选，扫描 %d 文件；旧格式候选须本人确认。\n", len(result.Messages), result.Files)
}
