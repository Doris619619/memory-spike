// 文件用途：保守清理 Codex 用户输入的附件包装与明显凭据；不宣称替代人工隐私检查。
package spike

import (
	"regexp"
	"strings"
)

var codexAutoTags = []string{"<environment_context", "<external_codex_apps_", "<in-app-browser-context", "<agent-message", "<subagent_notification", "<turn_aborted", "<permissions instructions", "<skills_instructions", "<system_reminder", "<user_instructions", "<context", "<image", "</image", "<audio", "<video"}
var codexSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:sk-(?:proj-|ant-)?[a-z0-9_-]{12,}|gh[pousr]_[a-z0-9_]{20,}|github_pat_[a-z0-9_]{20,}|AKIA[A-Z0-9]{16})\b`),
	regexp.MustCompile(`(?s)-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----.*?-----END (?:RSA |EC |OPENSSH )?PRIVATE KEY-----`),
	regexp.MustCompile(`\beyJ[a-zA-Z0-9_-]{8,}\.[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\b`),
	regexp.MustCompile(`(?i)\bBearer\s+[a-z0-9._~+/-]+=*`),
	regexp.MustCompile(`(?i)(?:密码|口令|密钥|password|passwd|api[_ -]?key|access[_ -]?token|secret)\s*(?:是|为|[:：=])\s*["']?[^\s,，。;；"'<>]{3,}`),
	regexp.MustCompile(`(?i)(?:https?|ssh|sqlserver|postgres(?:ql)?|mysql)://[^\s/@:]+:[^\s/@]+@[^\s]+`),
	regexp.MustCompile(`(?i)[?&](?:token|key|api_key|password|secret|access_token)=[^\s&#]+`),
	regexp.MustCompile(`\b[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}\b`),
	regexp.MustCompile(`\b[1-9][0-9]{16}[0-9Xx]\b`),
	regexp.MustCompile(`\b1[3-9][0-9]{9}\b`),
	regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`),
	regexp.MustCompile(`(?i)[A-Z]:\\Users\\[^\\\s]+`),
}

// cleanCodexText 只接受可辨识的普通文字；交互问答包装暂保守排除，附件仅保留 My request 后的本人请求。
func cleanCodexText(text string) (string, bool) {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if strings.HasPrefix(text, "# Files mentioned by the user:") {
		at := strings.Index(text, "## My request:")
		if at < 0 {
			return "", false
		}
		text = strings.TrimSpace(text[at+len("## My request:"):])
	}
	if text == "" || strings.HasPrefix(text, "# AGENTS.md instructions") || strings.HasPrefix(text, "<send_user_message_question_reply>") {
		return "", false
	}
	for _, tag := range codexAutoTags {
		if strings.HasPrefix(text, tag) {
			return "", false
		}
	}
	return text, true
}

// redactCodexText 替换常见凭据、邮箱和手机号，返回替换次数；自由文本住址/健康信息仍需本人检查。
func redactCodexText(text string) (string, int) {
	count := 0
	for _, pattern := range codexSecretPatterns {
		count += len(pattern.FindAllStringIndex(text, -1))
		text = pattern.ReplaceAllString(text, "[已遮盖敏感片段]")
	}
	return text, count
}
