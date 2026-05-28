package domain

// Status constants
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
	StatusError    = "error"
	StatusUnused   = "unused"
	StatusUsed     = "used"
	StatusExpired  = "expired"
)

// Role constants
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// Platform constants
const (
	PlatformAnthropic   = "anthropic"
	PlatformOpenAI      = "openai"
	PlatformGemini      = "gemini"
	PlatformAntigravity = "antigravity"
	PlatformKiro        = "kiro"
)

// Account type constants
const (
	AccountTypeOAuth          = "oauth"           // OAuth类型账号（full scope: profile + inference）
	AccountTypeSetupToken     = "setup-token"     // Setup Token类型账号（inference only scope）
	AccountTypeAPIKey         = "apikey"          // API Key类型账号
	AccountTypeUpstream       = "upstream"        // 上游透传类型账号（通过 Base URL + API Key 连接上游）
	AccountTypeBedrock        = "bedrock"         // AWS Bedrock 类型账号（通过 SigV4 签名或 API Key 连接 Bedrock，由 credentials.auth_mode 区分）
	AccountTypeServiceAccount = "service_account" // Google Service Account 类型账号（用于 Vertex AI）
)

// Redeem type constants
const (
	RedeemTypeBalance      = "balance"
	RedeemTypeConcurrency  = "concurrency"
	RedeemTypeSubscription = "subscription"
	RedeemTypeInvitation   = "invitation"
)

// PromoCode status constants
const (
	PromoCodeStatusActive   = "active"
	PromoCodeStatusDisabled = "disabled"
)

// Admin adjustment type constants
const (
	AdjustmentTypeAdminBalance     = "admin_balance"     // 管理员调整余额
	AdjustmentTypeAdminConcurrency = "admin_concurrency" // 管理员调整并发数
)

// Group subscription type constants
const (
	SubscriptionTypeStandard     = "standard"     // 标准计费模式（按余额扣费）
	SubscriptionTypeSubscription = "subscription" // 订阅模式（按限额控制）
)

// Subscription status constants
const (
	SubscriptionStatusActive    = "active"
	SubscriptionStatusExpired   = "expired"
	SubscriptionStatusSuspended = "suspended"
)

// DefaultAntigravityModelMapping 是 Antigravity 平台的默认模型映射。
// 只暴露当前实测可用的规范模型；历史别名由账号已有 model_mapping 兼容，默认列表不再展示。
// 与前端 useModelWhitelist.ts 中的 antigravityModels 保持一致。
var DefaultAntigravityModelMapping = map[string]string{
	"claude-opus-4-6-thinking":    "claude-opus-4-6-thinking",
	"claude-sonnet-4-6":           "claude-sonnet-4-6",
	"gemini-2.5-flash":            "gemini-2.5-flash",
	"gemini-2.5-flash-lite":       "gemini-2.5-flash-lite",
	"gemini-2.5-flash-thinking":   "gemini-2.5-flash-thinking",
	"gemini-3-flash":              "gemini-3-flash",
	"gemini-3-flash-agent":        "gemini-3-flash-agent",
	"gemini-3.1-flash-image":      "gemini-3.1-flash-image",
	"gemini-3.1-flash-lite":       "gemini-3.1-flash-lite",
	"gemini-pro-agent":            "gemini-pro-agent",
	"gemini-3.1-pro-low":          "gemini-3.1-pro-low",
	"gemini-3.5-flash-extra-low":  "gemini-3.5-flash-extra-low",
	"gemini-3.5-flash-low":        "gemini-3.5-flash-low",
	"gpt-oss-120b-medium":         "gpt-oss-120b-medium",
	"tab_flash_lite_preview":      "tab_flash_lite_preview",
	"tab_jump_flash_lite_preview": "tab_jump_flash_lite_preview",
}

// AntigravityCompatibilityModelMapping keeps old request IDs routable without exposing them in public lists.
var AntigravityCompatibilityModelMapping = map[string]string{
	"claude-opus-4-6":                "claude-opus-4-6-thinking",
	"claude-opus-4-5-thinking":       "claude-opus-4-6-thinking",
	"claude-opus-4-5-20251101":       "claude-opus-4-6-thinking",
	"claude-sonnet-4-5":              "claude-sonnet-4-6",
	"claude-sonnet-4-5-thinking":     "claude-sonnet-4-6",
	"claude-sonnet-4-5-20250929":     "claude-sonnet-4-6",
	"claude-haiku-4-5":               "claude-sonnet-4-6",
	"claude-haiku-4-5-20251001":      "claude-sonnet-4-6",
	"gemini-2.5-pro":                 "gemini-2.5-flash",
	"gemini-2.5-flash-image":         "gemini-3.1-flash-image",
	"gemini-2.5-flash-image-preview": "gemini-3.1-flash-image",
	"gemini-3-flash-preview":         "gemini-3-flash",
	"gemini-3-pro-high":              "gemini-pro-agent",
	"gemini-3-pro-low":               "gemini-3.1-pro-low",
	"gemini-3-pro-preview":           "gemini-pro-agent",
	"gemini-3-pro-image":             "gemini-3.1-flash-image",
	"gemini-3-pro-image-preview":     "gemini-3.1-flash-image",
	"gemini-3.1-pro-high":            "gemini-pro-agent",
	"gemini-3.1-pro-low":             "gemini-3.1-pro-low",
	"gemini-3.1-pro-preview":         "gemini-pro-agent",
	"gemini-pro-agent":               "gemini-pro-agent",
	"gemini-3.1-flash-image-preview": "gemini-3.1-flash-image",
}

// DefaultKiroModelMapping 是 Kiro 平台的默认模型映射。
// 键为对外暴露/允许请求的模型名，值为实际发送到 Kiro 上游的模型名。
var DefaultKiroModelMapping = map[string]string{
	"claude-opus-4-7":                     "claude-opus-4.7",
	"claude-opus-4-7-thinking":            "claude-opus-4.7",
	"claude-opus-4-6":                     "claude-opus-4.6",
	"claude-opus-4-6-thinking":            "claude-opus-4.6",
	"claude-sonnet-4-6":                   "claude-sonnet-4.6",
	"claude-sonnet-4-6-thinking":          "claude-sonnet-4.6",
	"claude-opus-4-5-20251101":            "claude-opus-4.5",
	"claude-opus-4-5-20251101-thinking":   "claude-opus-4.5",
	"claude-sonnet-4-5-20250929":          "claude-sonnet-4.5",
	"claude-sonnet-4-5-20250929-thinking": "claude-sonnet-4.5",
	"claude-haiku-4-5-20251001":           "claude-haiku-4.5",
	"claude-haiku-4-5-20251001-thinking":  "claude-haiku-4.5",
}

// DefaultBedrockModelMapping 是 AWS Bedrock 平台的默认模型映射
// 将 Anthropic 标准模型名映射到 Bedrock 模型 ID
// 注意：此处的 "us." 前缀仅为默认值，ResolveBedrockModelID 会根据账号配置的
// aws_region 自动调整为匹配的区域前缀（如 eu.、apac.、jp. 等）
var DefaultBedrockModelMapping = map[string]string{
	// Claude Opus
	"claude-opus-4-7":          "us.anthropic.claude-opus-4-7-v1",
	"claude-opus-4-6-thinking": "us.anthropic.claude-opus-4-6-v1",
	"claude-opus-4-6":          "us.anthropic.claude-opus-4-6-v1",
	"claude-opus-4-5-thinking": "us.anthropic.claude-opus-4-5-20251101-v1:0",
	"claude-opus-4-5-20251101": "us.anthropic.claude-opus-4-5-20251101-v1:0",
	"claude-opus-4-1":          "us.anthropic.claude-opus-4-1-20250805-v1:0",
	"claude-opus-4-20250514":   "us.anthropic.claude-opus-4-20250514-v1:0",
	// Claude Sonnet
	"claude-sonnet-4-6-thinking": "us.anthropic.claude-sonnet-4-6",
	"claude-sonnet-4-6":          "us.anthropic.claude-sonnet-4-6",
	"claude-sonnet-4-5":          "us.anthropic.claude-sonnet-4-5-20250929-v1:0",
	"claude-sonnet-4-5-thinking": "us.anthropic.claude-sonnet-4-5-20250929-v1:0",
	"claude-sonnet-4-5-20250929": "us.anthropic.claude-sonnet-4-5-20250929-v1:0",
	"claude-sonnet-4-20250514":   "us.anthropic.claude-sonnet-4-20250514-v1:0",
	// Claude Haiku
	"claude-haiku-4-5":          "us.anthropic.claude-haiku-4-5-20251001-v1:0",
	"claude-haiku-4-5-20251001": "us.anthropic.claude-haiku-4-5-20251001-v1:0",
}
