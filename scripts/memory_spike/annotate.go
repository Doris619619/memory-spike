//go:build ignore

// 文件用途：启动仓库外样本的本机人工标注工具，不调用模型或上传聊天。
package main

import (
	"flag"
	"fmt"
	"log"
	"memory-spike/internal/annotator"
	"net"
	"net/http"
	"time"
)

// main 显式选择样本与保存文件，端口冲突直接失败，不终止其他进程。
func main() {
	preview := flag.String("preview", "", "本人同意本地处理的 preview.json")
	out := flag.String("out", "", "仓库外 annotation-review.json 保存路径")
	port := flag.Int("port", 8787, "本机端口")
	flag.Parse()
	if *preview == "" || *out == "" {
		log.Fatal("请提供 -preview 与 -out")
	}
	store, err := annotator.Open(*preview, *out)
	if err != nil {
		log.Fatal(err)
	}
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("标注页面：http://%s\n进度保存：%s\n", listener.Addr(), *out)
	server := http.Server{Handler: annotator.Handler(store, listener.Addr().String()), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	log.Fatal(server.Serve(listener))
}
