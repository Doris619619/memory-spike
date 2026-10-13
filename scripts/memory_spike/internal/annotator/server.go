// 文件用途：提供仅本机可访问的标注页面及带来源检查、版本控制的保存接口。
package annotator

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
)

//go:embed web/*
var assets embed.FS

// Handler 固定 Host 防止 DNS 重绑定，令牌防止外部网页写入，不提供私有目录文件服务。
func Handler(store *Store, host string) http.Handler {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	token := hex.EncodeToString(nonce)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		if r.Host != host || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != "http://"+host) {
			http.Error(w, "仅允许本机页面访问", 403)
			return
		}
		switch {
		case r.URL.Path == "/api/state" && r.Method == "GET":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(map[string]any{"sample": store.Preview.Sample, "state": store.Snapshot(), "token": token})
		case r.URL.Path == "/api/state" && r.Method == "POST":
			if r.Header.Get("X-Annotation-Token") != token || r.Header.Get("Content-Type") != "application/json" {
				http.Error(w, "页面验证失败，请刷新后重试", 403)
				return
			}
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
			decoder.DisallowUnknownFields()
			var next State
			if decoder.Decode(&next) != nil || decoder.Decode(new(any)) != io.EOF {
				http.Error(w, "保存内容格式不正确或过大", 400)
				return
			}
			saved, err := store.Save(next)
			if err != nil {
				http.Error(w, err.Error(), 409)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(saved)
		case r.Method == "GET" && (r.URL.Path == "/" || r.URL.Path == "/app.js" || r.URL.Path == "/style.css"):
			name := r.URL.Path
			contentType := "text/html; charset=utf-8"
			if name == "/" {
				name = "/index.html"
			}
			if name == "/app.js" {
				contentType = "text/javascript; charset=utf-8"
			}
			if name == "/style.css" {
				contentType = "text/css; charset=utf-8"
			}
			b, _ := assets.ReadFile("web" + name)
			w.Header().Set("Content-Type", contentType)
			_, _ = w.Write(b)
		default:
			http.NotFound(w, r)
		}
	})
}
