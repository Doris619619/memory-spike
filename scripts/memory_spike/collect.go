//go:build ignore

// 文件用途：从获准的 SQL Server 只读连接收集单个匿名样本，不持久化 owner 映射。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"memory-spike/internal/spike"
	"os"
	"time"
)

// main 将敏感连接和 owner 限制在环境变量中；只输出匿名样本计数。
func main() {
	id := flag.String("sample", "", "匿名标签 U1/U2/U3/U6")
	out := flag.String("out", "", "仓库外输出 JSONL 文件")
	consent := flag.Bool("consent", false, "确认该用户已知情同意本次取样与模型评估")
	now := flag.String("now", time.Now().UTC().Format(time.RFC3339), "固定取样结束时刻 RFC3339")
	flag.Parse()
	if *id != "U1" && *id != "U2" && *id != "U3" && *id != "U6" {
		fail(fmt.Errorf("sample 必须为 U1/U2/U3/U6；U4/U5 由合成夹具准备"))
	}
	if !*consent || *out == "" {
		fail(fmt.Errorf("必须提供 -consent 和 -out"))
	}
	if err := spike.PrivatePath(*out); err != nil {
		fail(err)
	}
	if _, err := os.Stat(*out); !os.IsNotExist(err) {
		fail(fmt.Errorf("输出路径已经存在或不可访问"))
	}
	end, err := time.Parse(time.RFC3339, *now)
	if err != nil {
		fail(err)
	}
	dsn, owner := os.Getenv("MEMORY_SPIKE_SQL_DSN"), os.Getenv("MEMORY_SPIKE_OWNER")
	if dsn == "" || owner == "" {
		fail(fmt.Errorf("先设置 MEMORY_SPIKE_SQL_DSN 和 MEMORY_SPIKE_OWNER；不要把它们写进命令参数"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	sessions, err := spike.ReadDatabase(ctx, dsn, owner, end)
	if err != nil {
		fail(err)
	}
	sample, err := spike.Collect(sessions, *id, "real", true, end)
	if err != nil {
		fail(err)
	}
	b, err := json.Marshal(sample)
	if err != nil {
		fail(err)
	}
	if err := spike.WriteNew(*out, append(b, '\n')); err != nil {
		fail(err)
	}
	fmt.Printf("已保存 %s：%d 会话，%d 消息；sampled=%v\n", sample.ID, sample.SessionCount, sample.MessageCount, sample.Sampled)
}

// fail 以非零退出码报告已清理过的错误，不输出用户文本或连接凭据。
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
