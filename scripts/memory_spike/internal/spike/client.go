// 文件用途：用无重试、无重定向的 HTTP 请求执行 V0/V1，保存原始响应和实际 usage。
package spike

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// ExtractionSchema 返回与方案第 7 节完全同形的强制输出 schema，不包含生产 block ID。
func ExtractionSchema() map[string]any {
	evidence := map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "string"}}
	statement := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"text", "evidence_ids"}, "properties": map[string]any{"text": map[string]any{"type": "string"}, "evidence_ids": evidence}}
	block := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"section", "text", "evidence_ids"}, "properties": map[string]any{"section": map[string]any{"type": "string", "enum": []string{"沟通偏好", "背景与关注方向", "持续事项"}}, "text": map[string]any{"type": "string"}, "evidence_ids": evidence}}
	conflict := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"topic", "statements"}, "properties": map[string]any{"topic": map[string]any{"type": "string"}, "statements": map[string]any{"type": "array", "minItems": 2, "items": statement}}}
	return map[string]any{"type": "object", "additionalProperties": false, "required": []string{"blocks", "conflicts"}, "properties": map[string]any{"blocks": map[string]any{"type": "array", "items": block}, "conflicts": map[string]any{"type": "array", "items": conflict}}}
}

// RenderMessages 仅把取样消息提供给模型；金标和人工评分永远不进入请求。
func RenderMessages(s Sample) string {
	var b strings.Builder
	for _, m := range s.Messages {
		b.WriteString(m.ID + " | " + m.Date + " | 会话" + m.Session + " | " + m.Text + "\n")
	}
	return b.String()
}

// NewHTTPClient 禁止跟随 302 到 aTrust 等站点，且没有业务重试或自动模型回退。
func NewHTTPClient(seconds int) *http.Client {
	return &http.Client{Timeout: time.Duration(seconds) * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
}

// Call 对一个组合发送恰好一次请求；网络错误和截断保留为实验结果，不重发。
func Call(client *http.Client, c Config, m Model, variant, prompt, key string, s Sample) Run {
	r := Run{SampleID: s.ID, SampleHash: SampleHash(s), Provenance: s.Provenance, Model: m, Variant: variant, PromptVersion: c.PromptVersion, PromptHash: Hash([]byte(prompt)), MaxOutputTokens: c.MaxOutputTokens, StartedAt: time.Now().UTC().Format(time.RFC3339Nano), State: "finished", Validation: []string{}}
	if variant == "V0" && m.Structured == "unavailable" {
		r.State = "unavailable"
		return r
	}
	payload := map[string]any{"model": m.ID, "max_tokens": c.MaxOutputTokens, "stream": false}
	endpoint := strings.TrimRight(c.BaseURL, "/") + "/chat/completions"
	if m.APIType == "messages" {
		endpoint = strings.TrimRight(c.BaseURL, "/") + "/messages"
		payload["system"] = prompt
		payload["messages"] = []map[string]string{{"role": "user", "content": RenderMessages(s)}}
		if variant == "V0" {
			payload["tools"] = []any{map[string]any{"name": "extract_memory", "description": "输出候选记忆与冲突", "input_schema": ExtractionSchema()}}
			payload["tool_choice"] = map[string]any{"type": "tool", "name": "extract_memory", "disable_parallel_tool_use": true}
		}
	} else {
		payload["messages"] = []map[string]string{{"role": "system", "content": prompt}, {"role": "user", "content": RenderMessages(s)}}
		if variant == "V0" {
			if m.Structured == "json_schema" {
				payload["response_format"] = map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "memory_extraction", "strict": true, "schema": ExtractionSchema()}}
			} else {
				payload["response_format"] = map[string]any{"type": "json_object"}
			}
		}
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		r.State = "request_error"
		return r
	}
	req.Header.Set("Content-Type", "application/json")
	if m.APIType == "messages" {
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	start := time.Now()
	response, err := client.Do(req)
	if err != nil {
		r.LatencyMS = time.Since(start).Milliseconds()
		r.State = "transport_error"
		return r
	}
	defer response.Body.Close()
	r.HTTPStatus = response.StatusCode
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20+1))
	r.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		r.State = "read_error"
		return r
	}
	if len(raw) > 4<<20 {
		r.State = "response_too_large"
		return r
	}
	if key != "" {
		raw = bytes.ReplaceAll(raw, []byte(key), []byte("[REDACTED]"))
	}
	if json.Valid(raw) {
		r.Raw = raw
	} else {
		wrapped, _ := json.Marshal(map[string]string{"non_json_body": string(raw)})
		r.Raw = wrapped
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		r.State = "http_error"
		return r
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Finish string `json:"finish_reason"`
		} `json:"choices"`
		Content []struct {
			Type  string          `json:"type"`
			Name  string          `json:"name"`
			Text  string          `json:"text"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		StopReason string          `json:"stop_reason"`
		Usage      json.RawMessage `json:"usage"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		r.State = "protocol_error"
		return r
	}
	r.Usage = envelope.Usage
	if m.APIType == "messages" {
		r.FinishReason = envelope.StopReason
		tools := 0
		for _, part := range envelope.Content {
			if variant == "V0" && part.Type == "tool_use" && part.Name == "extract_memory" {
				tools++
				r.Text = string(part.Input)
			} else if variant == "V1" && part.Type == "text" {
				r.Text += part.Text
			}
		}
		if variant == "V0" && tools != 1 {
			r.State = "protocol_error"
			r.FormatFailure = "schema_mismatch"
			return r
		}
	} else if len(envelope.Choices) == 1 {
		r.Text = envelope.Choices[0].Message.Content
		r.FinishReason = envelope.Choices[0].Finish
	} else {
		r.State = "protocol_error"
		return r
	}
	var usage struct {
		Prompt     *int `json:"prompt_tokens"`
		Completion *int `json:"completion_tokens"`
		Input      *int `json:"input_tokens"`
		Output     *int `json:"output_tokens"`
	}
	if json.Unmarshal(r.Usage, &usage) == nil {
		if m.APIType == "messages" {
			r.InputTokens = usage.Input
			r.OutputTokens = usage.Output
		} else {
			r.InputTokens = usage.Prompt
			r.OutputTokens = usage.Completion
		}
	}
	if r.InputTokens != nil && r.OutputTokens != nil && m.InputPrice != nil && m.OutputPrice != nil {
		cost := (float64(*r.InputTokens)**m.InputPrice + float64(*r.OutputTokens)**m.OutputPrice) / 1e6
		r.EstimatedCost = &cost
	}
	r.Parsed, r.FormatFailure, r.Validation = ParseExtraction(r.Text, variant, s)
	if r.FinishReason == "length" || r.FinishReason == "max_tokens" {
		r.FormatFailure = "truncated"
	}
	return r
}
