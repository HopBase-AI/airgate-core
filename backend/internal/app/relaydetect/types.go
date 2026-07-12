package relaydetect

import "time"

const (
	CorePluginID = "airgate-core"
	TaskType     = "relay_detection"
)

type PlatformType string

const (
	PlatformAuto        PlatformType = "auto"
	PlatformAnthropic   PlatformType = "anthropic"
	PlatformOpenAI      PlatformType = "openai"
	PlatformAWSBedrock  PlatformType = "aws-bedrock"
	PlatformAWSPlatform PlatformType = "aws-platform"
	PlatformKiro        PlatformType = "kiro"
	PlatformWindsurf    PlatformType = "windsurf"
	PlatformClaudeCode  PlatformType = "claude-code"
)

type CreateRequest struct {
	BaseURL      string
	APIKey       string
	PlatformType PlatformType
	UserID       int
}

type TaskSummary struct {
	ID           int                    `json:"id"`
	Status       string                 `json:"status"`
	Stage        string                 `json:"stage"`
	Progress     int                    `json:"progress"`
	BaseURL      string                 `json:"base_url"`
	PlatformType string                 `json:"platform_type"`
	KeyHint      string                 `json:"key_hint"`
	OverallGrade string                 `json:"overall_grade"`
	ChannelLabel string                 `json:"channel_label"`
	Confidence   string                 `json:"confidence"`
	ModelCount   int                    `json:"model_count"`
	RiskCount    int                    `json:"risk_count"`
	ErrorMessage string                 `json:"error_message"`
	Output       map[string]interface{} `json:"output,omitempty"`
	Execution    map[string]interface{} `json:"execution,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	StartedAt    *time.Time             `json:"started_at,omitempty"`
	CompletedAt  *time.Time             `json:"completed_at,omitempty"`
}

type Report struct {
	Version        string           `json:"version"`
	BaseURL        string           `json:"base_url"`
	PlatformType   string           `json:"platform_type"`
	StartedAt      string           `json:"started_at"`
	CompletedAt    string           `json:"completed_at,omitempty"`
	Summary        ReportSummary    `json:"summary"`
	ModelCatalog   ModelCatalog     `json:"model_catalog"`
	Models         []ModelResult    `json:"models"`
	ModelMatrix    []ModelMatrixRow `json:"model_issue_matrix,omitempty"`
	Risks          []RiskFinding    `json:"risks"`
	Evidence       []EvidenceItem   `json:"evidence"`
	StandardChecks []StandardCheck  `json:"standard_checks"`
	Baselines      []BaselineDiff   `json:"baselines,omitempty"`
	Charts         ChartData        `json:"charts"`
	Raw            map[string]any   `json:"raw,omitempty"`
	NextMilestone  []string         `json:"next_milestone,omitempty"`
}

type ReportSummary struct {
	OverallGrade           string          `json:"overall_grade"`
	OverallScore           float64         `json:"overall_score"`
	ScoreEligible          bool            `json:"score_eligible"`
	ScoreEligibilityReason string          `json:"score_eligibility_reason,omitempty"`
	ChannelLabel           string          `json:"channel_label"`
	Confidence             string          `json:"confidence"`
	ProductionReady        bool            `json:"production_ready"`
	ModelCount             int             `json:"model_count"`
	AvailableModels        int             `json:"available_models"`
	RiskModels             int             `json:"risk_models"`
	AverageLatencyMS       float64         `json:"average_latency_ms"`
	AverageInjection       float64         `json:"average_injection_tokens"`
	Coverage               CoverageSummary `json:"coverage"`
}

type CoverageSummary struct {
	Applicable    int     `json:"applicable"`
	Attempted     int     `json:"attempted"`
	Conclusive    int     `json:"conclusive"`
	Blocked       int     `json:"blocked"`
	NotRun        int     `json:"not_run"`
	NotApplicable int     `json:"not_applicable"`
	Ratio         float64 `json:"ratio"`
}

type ModelCatalog struct {
	Route         string         `json:"route"`
	HTTPStatus    int            `json:"http_status"`
	Total         int            `json:"total"`
	Families      map[string]int `json:"families"`
	Synthetic     bool           `json:"synthetic"`
	Heterogeneous bool           `json:"heterogeneous"`
}

type ModelResult struct {
	Model                 string               `json:"model"`
	Family                string               `json:"family"`
	Available             bool                 `json:"available"`
	Grade                 string               `json:"grade"`
	Protocol              string               `json:"protocol"`
	HTTPStatus            int                  `json:"http_status"`
	ResponseID            string               `json:"response_id,omitempty"`
	ResponseIDPrefix      string               `json:"response_id_prefix,omitempty"`
	RequestedModel        string               `json:"requested_model"`
	ReturnedModel         string               `json:"returned_model,omitempty"`
	ModelMatched          bool                 `json:"model_matched"`
	ModelMatchKind        string               `json:"model_match_kind"`
	ModelMatchReason      string               `json:"model_match_reason,omitempty"`
	InputTokens           int                  `json:"input_tokens"`
	OutputTokens          int                  `json:"output_tokens"`
	CacheCreationTokens   int                  `json:"cache_creation_tokens"`
	CacheReadTokens       int                  `json:"cache_read_tokens"`
	HiddenInjectionTokens int                  `json:"hidden_injection_tokens"`
	UsageFields           []string             `json:"usage_fields"`
	LatencyMS             int64                `json:"latency_ms"`
	Stream                StreamProbe          `json:"stream"`
	Cache                 CacheProbe           `json:"cache"`
	CacheTTL              CacheTTLProbe        `json:"cache_ttl"`
	Injection             InjectionProbe       `json:"injection"`
	Quality               QualityProbe         `json:"quality"`
	RoleProbe             RoleProbe            `json:"role_probe"`
	Thinking              ThinkingProbe        `json:"thinking_probe"`
	TokenPrecision        TokenPrecision       `json:"token_precision"`
	RuntimeBaseline       RuntimeBaselineProbe `json:"runtime_baseline,omitempty"`
	AnthropicCountTokens  AnthropicCountTokens `json:"anthropic_count_tokens,omitempty"`
	OpenAINative          OpenAINativeProbe    `json:"openai_native,omitempty"`
	SourceProbe           SourceProbe          `json:"source_probe"`
	Stability             StabilityProbe       `json:"stability"`
	ClientProfiles        []ClientProfileProbe `json:"client_profiles,omitempty"`
	CCGate                CCGateProbe          `json:"cc_gate,omitempty"`
	Risks                 []string             `json:"risks"`
	Error                 string               `json:"error,omitempty"`
	Headers               map[string]any       `json:"headers,omitempty"`
	Transport             TransportEvidence    `json:"transport,omitempty"`
}

type TransportEvidence struct {
	Method              string            `json:"method,omitempty"`
	URL                 string            `json:"url,omitempty"`
	Host                string            `json:"host,omitempty"`
	SNI                 string            `json:"sni,omitempty"`
	TLSServerName       string            `json:"tls_server_name,omitempty"`
	TLSSANs             []string          `json:"tls_sans,omitempty"`
	RequestHeaders      map[string]string `json:"request_headers,omitempty"`
	ResponseHeaders     map[string]string `json:"response_headers,omitempty"`
	RequestID           string            `json:"request_id,omitempty"`
	RateLimitHeaders    map[string]string `json:"rate_limit_headers,omitempty"`
	PromptPayloadHash   string            `json:"prompt_payload_hash,omitempty"`
	ResponseBodyHash    string            `json:"response_body_hash,omitempty"`
	ResponseBodySize    int               `json:"response_body_size,omitempty"`
	ErrorBodySummary    string            `json:"error_body_summary,omitempty"`
	RawStreamSummary    string            `json:"raw_stream_summary,omitempty"`
	ConnectedRemoteAddr string            `json:"connected_remote_addr,omitempty"`
}

type StreamProbe struct {
	Tested      bool              `json:"tested"`
	OK          bool              `json:"ok"`
	HTTPStatus  int               `json:"http_status"`
	ContentType string            `json:"content_type,omitempty"`
	EventCount  int               `json:"event_count"`
	Events      []string          `json:"events,omitempty"`
	HasDone     bool              `json:"has_done"`
	HasUsage    bool              `json:"has_usage"`
	TTFBMS      int64             `json:"ttfb_ms"`
	LatencyMS   int64             `json:"latency_ms"`
	Error       string            `json:"error,omitempty"`
	Transport   TransportEvidence `json:"transport,omitempty"`
}

type CacheProbe struct {
	Tested         bool         `json:"tested"`
	Applicable     bool         `json:"applicable"`
	OK             bool         `json:"ok"`
	Protocol       string       `json:"protocol,omitempty"`
	CostSemantics  string       `json:"cost_semantics,omitempty"`
	Rounds         int          `json:"rounds"`
	HasCacheFields bool         `json:"has_cache_fields"`
	CacheEngaged   bool         `json:"cache_engaged"`
	WarmHitRate    float64      `json:"warm_hit_rate"`
	FirstReadRound int          `json:"first_read_round"`
	CollapseRounds []int        `json:"collapse_rounds,omitempty"`
	BurnFactor     float64      `json:"burn_factor"`
	RoundResults   []CacheRound `json:"round_results,omitempty"`
	Error          string       `json:"error,omitempty"`
}

type CacheRound struct {
	Round               int    `json:"round"`
	OK                  bool   `json:"ok"`
	HTTPStatus          int    `json:"http_status"`
	HasCacheFields      bool   `json:"has_cache_fields"`
	InputTokens         int    `json:"input_tokens"`
	CacheCreationTokens int    `json:"cache_creation_tokens"`
	CacheReadTokens     int    `json:"cache_read_tokens"`
	LatencyMS           int64  `json:"latency_ms"`
	Error               string `json:"error,omitempty"`
}

type CacheTTLProbe struct {
	Tested         bool             `json:"tested"`
	Applicable     bool             `json:"applicable"`
	OK             bool             `json:"ok"`
	Supports5M     bool             `json:"supports_5m"`
	Supports1H     bool             `json:"supports_1h"`
	RejectsInvalid bool             `json:"rejects_invalid"`
	Configurations []CacheTTLResult `json:"configurations,omitempty"`
	Error          string           `json:"error,omitempty"`
}

type CacheTTLResult struct {
	Name                  string `json:"name"`
	RequestedTTL          string `json:"requested_ttl,omitempty"`
	Expected              string `json:"expected"`
	OK                    bool   `json:"ok"`
	HTTPStatus            int    `json:"http_status"`
	CacheCreation5MTokens int    `json:"cache_creation_5m_tokens"`
	CacheCreation1HTokens int    `json:"cache_creation_1h_tokens"`
	CacheReadTokens       int    `json:"cache_read_tokens"`
	Error                 string `json:"error,omitempty"`
}

type InjectionProbe struct {
	Tested           bool                `json:"tested"`
	OK               bool                `json:"ok"`
	TokenEstimate    int                 `json:"token_estimate"`
	KeywordHits      []string            `json:"keyword_hits,omitempty"`
	IdentityConflict bool                `json:"identity_conflict"`
	CanaryLeaked     bool                `json:"canary_leaked"`
	PromptDisclosure bool                `json:"prompt_disclosure"`
	Samples          []PromptProbeSample `json:"samples,omitempty"`
}

type RoleProbe struct {
	Tested           bool                `json:"tested"`
	OK               bool                `json:"ok"`
	IdentityConflict bool                `json:"identity_conflict"`
	Samples          []PromptProbeSample `json:"samples,omitempty"`
	Error            string              `json:"error,omitempty"`
}

type ThinkingProbe struct {
	Tested                bool              `json:"tested"`
	OK                    bool              `json:"ok"`
	Supported             bool              `json:"supported"`
	Requested             bool              `json:"requested"`
	HTTPStatus            int               `json:"http_status"`
	HasThinkingContent    bool              `json:"has_thinking_content"`
	HasSignatureDelta     bool              `json:"has_signature_delta"`
	SignatureStructureOK  bool              `json:"signature_structure_ok"`
	EventOrderOK          bool              `json:"event_order_ok"`
	RuntimeRoundTripOK    bool              `json:"runtime_round_trip_ok"`
	TamperRejected        bool              `json:"tamper_rejected"`
	FakeSignatureRejected bool              `json:"fake_signature_rejected"`
	ToolContinuationOK    bool              `json:"tool_continuation_ok"`
	Events                []string          `json:"events,omitempty"`
	RuntimeChecks         []RuntimeCheck    `json:"runtime_checks,omitempty"`
	Error                 string            `json:"error,omitempty"`
	Transport             TransportEvidence `json:"transport,omitempty"`
	thinkingBlocks        []map[string]any
}

type RuntimeCheck struct {
	Name       string         `json:"name"`
	OK         bool           `json:"ok"`
	HTTPStatus int            `json:"http_status,omitempty"`
	Detail     map[string]any `json:"detail,omitempty"`
	Error      string         `json:"error,omitempty"`
}

// CCGateProbe 是 cc-vs-plain 差分闸门结果:用真实 plain SDK 身份与真实 Claude Code 身份
// (真 UA + anthropic-beta + "You are Claude Code" system 首块)打同一条裸请求,识别
// "只认 Claude Code 指纹的订阅号池"。这是标准里的 #2 决定性反作弊信号,不需要第二把真值 key。
type CCGateProbe struct {
	Tested           bool   `json:"tested"`
	PlainStatus      int    `json:"plain_status"`
	CCStatus         int    `json:"cc_status"`
	PlainAvailable   bool   `json:"plain_available"`
	CCAvailable      bool   `json:"cc_available"`
	PlainHasID       bool   `json:"plain_has_id"`
	PlainHasUsage    bool   `json:"plain_has_usage"`
	ForgedCCGate     bool   `json:"forged_cc_gate"`
	PlainGated       bool   `json:"plain_gated"`
	Verdict          string `json:"verdict"`
	PlainBodyExcerpt string `json:"plain_body_excerpt,omitempty"`
}

type ClientProfileProbe struct {
	ProfileID     string            `json:"profile_id"`
	Title         string            `json:"title"`
	Tested        bool              `json:"tested"`
	OK            bool              `json:"ok"`
	Scenario      string            `json:"scenario"`
	HTTPStatus    int               `json:"http_status,omitempty"`
	StreamOK      bool              `json:"stream_ok"`
	ThinkingOK    bool              `json:"thinking_ok"`
	SubagentsOK   bool              `json:"subagents_ok"`
	CacheOK       bool              `json:"cache_ok"`
	Cache         CacheProbe        `json:"cache,omitempty"`
	SuccessRate   float64           `json:"success_rate,omitempty"`
	LatencyMS     int64             `json:"latency_ms,omitempty"`
	Error         string            `json:"error,omitempty"`
	Transport     TransportEvidence `json:"transport,omitempty"`
	RuntimeChecks []RuntimeCheck    `json:"runtime_checks,omitempty"`
}

type TokenPrecision struct {
	Tested              bool   `json:"tested"`
	OK                  bool   `json:"ok"`
	ScoreEligible       bool   `json:"score_eligible"`
	BaselineSource      string `json:"baseline_source,omitempty"`
	Confidence          string `json:"confidence,omitempty"`
	ExpectedInputTokens int    `json:"expected_input_tokens"`
	ObservedInputTokens int    `json:"observed_input_tokens"`
	Delta               int    `json:"delta"`
	Error               string `json:"error,omitempty"`
}

type QualityProbe struct {
	Tested      bool          `json:"tested"`
	Applicable  bool          `json:"applicable"`
	OK          bool          `json:"ok"`
	Passed      int           `json:"passed"`
	Total       int           `json:"total"`
	SuccessRate float64       `json:"success_rate"`
	Cases       []QualityCase `json:"cases,omitempty"`
	Error       string        `json:"error,omitempty"`
}

type QualityCase struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	OK         bool   `json:"ok"`
	HTTPStatus int    `json:"http_status"`
	Output     string `json:"output,omitempty"`
	Error      string `json:"error,omitempty"`
}

type RuntimeBaselineProbe struct {
	Tested              bool              `json:"tested"`
	Configured          bool              `json:"configured"`
	Provider            string            `json:"provider,omitempty"`
	Protocol            string            `json:"protocol,omitempty"`
	ModelID             string            `json:"model_id,omitempty"`
	Region              string            `json:"region,omitempty"`
	Endpoint            string            `json:"endpoint,omitempty"`
	HTTPStatus          int               `json:"http_status,omitempty"`
	OfficialInputTokens int               `json:"official_input_tokens,omitempty"`
	ObservedInputTokens int               `json:"observed_input_tokens,omitempty"`
	Delta               int               `json:"delta,omitempty"`
	OK                  bool              `json:"ok"`
	Source              string            `json:"source,omitempty"`
	Error               string            `json:"error,omitempty"`
	Transport           TransportEvidence `json:"transport,omitempty"`
}

type AnthropicCountTokens struct {
	Tested             bool              `json:"tested"`
	OK                 bool              `json:"ok"`
	ShortHTTPStatus    int               `json:"short_http_status,omitempty"`
	ShortInputTokens   int               `json:"short_input_tokens,omitempty"`
	ObservedShortUsage int               `json:"observed_short_usage,omitempty"`
	ShortDelta         int               `json:"short_delta,omitempty"`
	CacheHTTPStatus    int               `json:"cache_http_status,omitempty"`
	CacheInputTokens   int               `json:"cache_input_tokens,omitempty"`
	Error              string            `json:"error,omitempty"`
	Transport          TransportEvidence `json:"transport,omitempty"`
}

type OpenAINativeProbe struct {
	ResponsesTested      bool              `json:"responses_tested"`
	ResponsesOK          bool              `json:"responses_ok"`
	ResponsesHTTPStatus  int               `json:"responses_http_status,omitempty"`
	ResponsesID          string            `json:"responses_id,omitempty"`
	ResponsesObject      string            `json:"responses_object,omitempty"`
	InputTokensTested    bool              `json:"input_tokens_tested"`
	InputTokensOK        bool              `json:"input_tokens_ok"`
	InputTokens          int               `json:"input_tokens,omitempty"`
	ToolCallTested       bool              `json:"tool_call_tested"`
	ToolCallOK           bool              `json:"tool_call_ok"`
	ToolCallHTTPStatus   int               `json:"tool_call_http_status,omitempty"`
	ToolCallName         string            `json:"tool_call_name,omitempty"`
	ToolCallArguments    map[string]any    `json:"tool_call_arguments,omitempty"`
	StructuredTested     bool              `json:"structured_tested"`
	StructuredOK         bool              `json:"structured_ok"`
	StructuredHTTPStatus int               `json:"structured_http_status,omitempty"`
	StructuredOutput     map[string]any    `json:"structured_output,omitempty"`
	Error                string            `json:"error,omitempty"`
	Transport            TransportEvidence `json:"transport,omitempty"`
}

type SourceProbe struct {
	Tested        bool   `json:"tested"`
	OK            bool   `json:"ok"`
	Expected      string `json:"expected,omitempty"`
	ClaimedSource string `json:"claimed_source,omitempty"`
	Text          string `json:"text,omitempty"`
	Error         string `json:"error,omitempty"`
}

type PromptProbeSample struct {
	Name        string   `json:"name"`
	OK          bool     `json:"ok"`
	HTTPStatus  int      `json:"http_status"`
	Text        string   `json:"text,omitempty"`
	InputTokens int      `json:"input_tokens"`
	KeywordHits []string `json:"keyword_hits,omitempty"`
	Error       string   `json:"error,omitempty"`
}

type StabilityProbe struct {
	Tested        bool               `json:"tested"`
	OK            bool               `json:"ok"`
	Rounds        int                `json:"rounds"`
	Success       int                `json:"success"`
	SuccessRate   float64            `json:"success_rate"`
	P50MS         int64              `json:"p50_ms"`
	P95MS         int64              `json:"p95_ms"`
	MaxMS         int64              `json:"max_ms"`
	ErrorClasses  map[string]int     `json:"error_classes,omitempty"`
	Concurrency   []ConcurrencyProbe `json:"concurrency,omitempty"`
	Windows       []StabilityWindow  `json:"windows,omitempty"`
	WindowSummary string             `json:"window_summary,omitempty"`
}

type StabilityWindow struct {
	Index        int            `json:"index"`
	Label        string         `json:"label"`
	Rounds       int            `json:"rounds"`
	Success      int            `json:"success"`
	SuccessRate  float64        `json:"success_rate"`
	P50MS        int64          `json:"p50_ms"`
	P95MS        int64          `json:"p95_ms"`
	MaxMS        int64          `json:"max_ms"`
	ErrorClasses map[string]int `json:"error_classes,omitempty"`
}

type ConcurrencyProbe struct {
	Level        int            `json:"level"`
	Success      int            `json:"success"`
	SuccessRate  float64        `json:"success_rate"`
	WallMS       int64          `json:"wall_ms"`
	P50MS        int64          `json:"p50_ms"`
	MaxMS        int64          `json:"max_ms"`
	ErrorClasses map[string]int `json:"error_classes,omitempty"`
}

type RiskFinding struct {
	Severity string         `json:"severity"`
	Code     string         `json:"code"`
	Message  string         `json:"message"`
	Model    string         `json:"model,omitempty"`
	Detail   map[string]any `json:"detail,omitempty"`
}

type ModelMatrixRow struct {
	Model         string            `json:"model"`
	Family        string            `json:"family"`
	Protocol      string            `json:"protocol"`
	Available     bool              `json:"available"`
	Grade         string            `json:"grade"`
	OverallStatus string            `json:"overall_status"`
	OverallReason string            `json:"overall_reason,omitempty"`
	Checks        []ModelMatrixCell `json:"checks"`
}

type ModelMatrixCell struct {
	ID                string             `json:"id"`
	Title             string             `json:"title"`
	Status            string             `json:"status"`
	Severity          string             `json:"severity,omitempty"`
	Summary           string             `json:"summary"`
	Metrics           map[string]any     `json:"metrics,omitempty"`
	Evidence          []string           `json:"evidence,omitempty"`
	EvidenceRefs      []ModelEvidenceRef `json:"evidence_refs,omitempty"`
	Risks             []string           `json:"risks,omitempty"`
	Applicable        bool               `json:"applicable"`
	Executed          bool               `json:"executed"`
	Conclusive        bool               `json:"conclusive"`
	ScoreEligible     bool               `json:"score_eligible"`
	EligibilityReason string             `json:"eligibility_reason,omitempty"`
	ScoreWeight       float64            `json:"score_weight,omitempty"`
	ScoreImpact       float64            `json:"score_impact,omitempty"`
}

type ModelEvidenceRef struct {
	Kind    string         `json:"kind"`
	Label   string         `json:"label"`
	Path    string         `json:"path"`
	Summary string         `json:"summary,omitempty"`
	Value   any            `json:"value,omitempty"`
	Detail  map[string]any `json:"detail,omitempty"`
}

type EvidenceItem struct {
	Strength string         `json:"strength"`
	Code     string         `json:"code"`
	Message  string         `json:"message"`
	Detail   map[string]any `json:"detail,omitempty"`
}

type StandardCheck struct {
	ID                string         `json:"id"`
	Category          string         `json:"category"`
	Title             string         `json:"title"`
	Status            string         `json:"status"`
	Severity          string         `json:"severity"`
	Conclusion        string         `json:"conclusion"`
	Evidence          []string       `json:"evidence,omitempty"`
	Metrics           map[string]any `json:"metrics,omitempty"`
	Missing           []string       `json:"missing,omitempty"`
	Source            string         `json:"source"`
	Applicable        bool           `json:"applicable"`
	Executed          bool           `json:"executed"`
	Conclusive        bool           `json:"conclusive"`
	ScoreEligible     bool           `json:"score_eligible"`
	EligibilityReason string         `json:"eligibility_reason,omitempty"`
	ScoreWeight       float64        `json:"score_weight,omitempty"`
	ScoreImpact       float64        `json:"score_impact,omitempty"`
}

type BaselineDiff struct {
	Kind        string         `json:"kind"`
	Provider    string         `json:"provider"`
	Protocol    string         `json:"protocol"`
	Source      string         `json:"source"`
	Model       string         `json:"model,omitempty"`
	Status      string         `json:"status"`
	Severity    string         `json:"severity"`
	Conclusion  string         `json:"conclusion"`
	Differences []string       `json:"differences,omitempty"`
	Metrics     map[string]any `json:"metrics,omitempty"`
}

type ChartData struct {
	GradeDistribution  []NameValue       `json:"grade_distribution"`
	FamilyDistribution []NameValue       `json:"family_distribution"`
	RiskDistribution   []NameValue       `json:"risk_distribution"`
	ModelMetrics       []ModelMetricItem `json:"model_metrics"`
}

type NameValue struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

type ModelMetricItem struct {
	Model           string  `json:"model"`
	LatencyMS       int64   `json:"latency_ms"`
	InputTokens     int     `json:"input_tokens"`
	InjectionTokens int     `json:"injection_tokens"`
	CacheTokens     int     `json:"cache_tokens"`
	Score           float64 `json:"score"`
}
