// 文件用途：定义取样、运行配置、模型结果和人工标注的数据格式。
package spike

import "encoding/json"

type Message struct {
	ID      string `json:"id"`
	Session string `json:"session"`
	Date    string `json:"date"`
	Text    string `json:"text"`
}

type Sample struct {
	ID               string    `json:"sample_id"`
	WindowStart      string    `json:"window_start"`
	WindowEnd        string    `json:"window_end"`
	SessionCount     int       `json:"session_count"`
	MessageCount     int       `json:"message_count"`
	Sampled          bool      `json:"sampled"`
	Messages         []Message `json:"messages"`
	Provenance       string    `json:"provenance"`
	Consented        bool      `json:"consented"`
	EligibleSessions int       `json:"eligible_sessions"`
	EligibleMessages int       `json:"eligible_messages"`
	ApproxTokens     float64   `json:"approx_tokens"`
	Warnings         []string  `json:"warnings,omitempty"`
}

type Gold struct {
	ID        string   `json:"id"`
	Fact      string   `json:"fact"`
	Category  string   `json:"category"`
	Evidence  []string `json:"evidence"`
	Stability string   `json:"stability"`
	Scope     string   `json:"scope,omitempty"`
	Project   string   `json:"project,omitempty"`
}

type Annotation struct {
	Method     string `json:"method,omitempty"`
	SampleID   string `json:"sample_id"`
	SampleHash string `json:"sample_sha256"`
	Annotator  string `json:"annotator"`
	Complete   bool   `json:"complete"`
	Entries    []Gold `json:"entries"`
}

type Model struct {
	ID                 string   `json:"id"`
	Provider           string   `json:"provider"`
	APIType            string   `json:"api_type"`
	Class              string   `json:"class"`
	Structured         string   `json:"structured_output"`
	CapabilityEvidence string   `json:"capability_evidence"`
	InputPrice         *float64 `json:"input_per_million"`
	OutputPrice        *float64 `json:"output_per_million"`
	Currency           string   `json:"currency"`
	PriceSource        string   `json:"price_source"`
	APIKeyEnv          string   `json:"api_key_env"`
}

type Config struct {
	Description     string   `json:"description"`
	BaseURL         string   `json:"base_url"`
	PromptVersion   string   `json:"prompt_version"`
	MaxOutputTokens int      `json:"max_output_tokens"`
	TimeoutSeconds  int      `json:"timeout_seconds"`
	Variants        []string `json:"variants"`
	Models          []Model  `json:"models"`
}

type Block struct {
	Section  string   `json:"section"`
	Text     string   `json:"text"`
	Evidence []string `json:"evidence_ids"`
}

type Statement struct {
	Text     string   `json:"text"`
	Evidence []string `json:"evidence_ids"`
}

type Conflict struct {
	Topic      string      `json:"topic"`
	Statements []Statement `json:"statements"`
}

type Extraction struct {
	Blocks    []Block    `json:"blocks"`
	Conflicts []Conflict `json:"conflicts"`
}

type Run struct {
	ID              string          `json:"run_id"`
	SampleID        string          `json:"sample_id"`
	SampleHash      string          `json:"sample_sha256"`
	Provenance      string          `json:"provenance"`
	Model           Model           `json:"model_config"`
	Variant         string          `json:"variant"`
	PromptVersion   string          `json:"prompt_version"`
	PromptHash      string          `json:"prompt_sha256"`
	ConfigHash      string          `json:"config_sha256"`
	MaxOutputTokens int             `json:"max_output_tokens"`
	StartedAt       string          `json:"started_at"`
	State           string          `json:"state"`
	HTTPStatus      int             `json:"http_status"`
	LatencyMS       int64           `json:"latency_ms"`
	Raw             json.RawMessage `json:"raw_response,omitempty"`
	Text            string          `json:"raw_output"`
	Parsed          *Extraction     `json:"parsed,omitempty"`
	FormatFailure   string          `json:"format_failure,omitempty"`
	Validation      []string        `json:"validation_errors"`
	FinishReason    string          `json:"finish_reason,omitempty"`
	Usage           json.RawMessage `json:"usage,omitempty"`
	InputTokens     *int            `json:"input_tokens"`
	OutputTokens    *int            `json:"output_tokens"`
	EstimatedCost   *float64        `json:"estimated_cost"`
}
