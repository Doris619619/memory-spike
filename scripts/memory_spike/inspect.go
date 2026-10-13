//go:build ignore

// 文件用途：只检查网站 JSON 导出的结构是否含取样元数据，不输出聊天正文、不调用模型。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
)

// main 接收用户主动导出的 JSON，报告字段是否齐全；缺失的渠道和 parent 元数据绝不推断。
func main() {
	path := flag.String("file", "", "用户导出的 JSON 文件")
	flag.Parse()
	if *path == "" {
		fail(fmt.Errorf("请指定 -file"))
	}
	f, err := os.Open(*path)
	if err != nil {
		fail(err)
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		fail(err)
	}
	if stat.Size() > 128<<20 {
		fail(fmt.Errorf("文件超过 128 MiB，请分批检查"))
	}
	var root any
	if err = json.NewDecoder(f).Decode(&root); err != nil {
		fail(fmt.Errorf("不是有效 JSON 导出"))
	}
	records, ok := root.([]any)
	if !ok {
		if obj, ok := root.(map[string]any); ok {
			records = []any{obj}
		} else {
			fail(fmt.Errorf("顶层不是会话对象或数组"))
		}
	}
	fmt.Printf("会话对象数量：%d\n", len(records))
	missing := map[string]int{}
	fields := map[string]bool{}
	for _, record := range records {
		obj, ok := record.(map[string]any)
		if !ok {
			fail(fmt.Errorf("数组含非会话对象"))
		}
		for k := range obj {
			fields[k] = true
		}
		for _, key := range []string{"chat_session_id", "source", "project_id", "parent_session_id", "updated_at", "layout"} {
			if _, ok := obj[key]; !ok {
				missing[key]++
			}
		}
	}
	names := []string{}
	for k := range fields {
		names = append(names, k)
	}
	sort.Strings(names)
	fmt.Println("顶层字段：", names)
	for _, key := range []string{"chat_session_id", "source", "project_id", "parent_session_id", "updated_at", "layout"} {
		if n := missing[key]; n > 0 {
			fmt.Printf("待补充 %s：%d 个会话\n", key, n)
		}
	}
	fmt.Println("仅完成结构检查；还需核对消息树、content.created_at、当前分支和取样完整性。字段不齐不能作为严格 SQL 取样的等价替代。")
}

// fail 不回显文件正文，避免用户原文进入终端日志。
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
