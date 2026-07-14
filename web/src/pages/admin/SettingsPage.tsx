import { type FormEvent, useState, useEffect, useRef, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Alert, AlertDialog, Button, Card, Form, Input, Label, Modal, Spinner, Tabs, TextArea, useOverlayState } from '@heroui/react';
import { DialogTriggerShim } from '../../shared/components/DialogTriggerShim';
import { settingsApi } from '../../shared/api/settings';
import { modelsApi, type BuiltinModel } from '../../shared/api/models';
import { adminApiKeyApi, type AdminAPIKeyResp } from '../../shared/api/adminApiKey';
import { defaultLogoUrl } from '../../app/providers/SiteSettingsProvider';
import { useCrudMutation } from '../../shared/hooks/useCrudMutation';
import { useClipboard } from '../../shared/hooks/useClipboard';
import { queryKeys } from '../../shared/queryKeys';
import { useToast } from '../../shared/ui';
import {
  Save, Loader2, Globe, Mail, MailSearch, Send, Upload, X, RotateCcw,
  ShieldCheck, Copy, Trash2, KeyRound, Zap, Download, Database, Boxes, Plus, ChevronDown, Info,
} from 'lucide-react';
import type { SettingItem, TestSMTPReq } from '../../shared/types';
import { SystemUpdatePanel } from './SystemUpdatePanel';
import { NativeSwitch } from '../../shared/components/NativeSwitch';
import { CommonModal } from '../../shared/components/CommonModal';

// ==================== 设置 key 定义 ====================

const SITE_KEYS = [
  'site_name', 'site_subtitle', 'site_logo', 'api_base_url',
  'contact_info', 'doc_url', 'landing_pricing_json',
  // 多落地页品牌覆盖（siteId → { name, logo }）：登录页/控制台按 ?site= 来源站切换品牌
  'sites_branding',
  // 整站通知横幅（放 site 组：随站点 tab 保存，且 site 组全量走公开设置接口）
  'announcement_enabled', 'announcement_level', 'announcement_content',
] as const;

const REG_KEYS = [
  'registration_enabled', 'email_verify_enabled',
  'registration_email_suffix_whitelist',
] as const;

const DEFAULT_KEYS = [
  'default_balance', 'default_concurrency',
] as const;

// 第三方登录（OAuth）配置：与安全 tab 一起保存，group=oauth。
// 公开设置只暴露 *_enabled 开关，client_id/secret 仅管理员接口可见。
const OAUTH_KEYS = [
  'oauth_google_enabled', 'oauth_google_client_id', 'oauth_google_client_secret',
  'oauth_github_enabled', 'oauth_github_client_id', 'oauth_github_client_secret',
] as const;

const OAUTH_PROVIDERS = [
  { id: 'google', label: 'Google' },
  { id: 'github', label: 'GitHub' },
] as const;

const SMTP_KEYS = [
  'smtp_host', 'smtp_port', 'smtp_username', 'smtp_password',
  'smtp_from_email', 'smtp_from_name', 'smtp_use_tls',
  'email_template_subject', 'email_template_body',
  'balance_alert_email_subject', 'balance_alert_email_body',
] as const;

const STORAGE_KEYS = [
  's3_endpoint', 's3_bucket', 's3_access_key', 's3_secret_key',
  's3_region', 's3_use_ssl', 's3_public_base_url',
  's3_presign_ttl_minutes', 's3_path_prefix', 'local_storage_dir',
  'asset_retention_generated_days',
] as const;

// OpenClaw 一键接入相关 setting key。所有 key 统一加 "openclaw." 前缀，便于在 Setting 表中识别。
// 默认值（DEFAULT_OPENCLAW_*）在后端 internal/app/openclaw/defaults.go 中维护了同构的一份，
// 这里只负责前端展示 / 回填。keep in sync。
// 模型目录覆盖层:每平台一个 JSON key。值 = 覆盖条目数组。
// 存储层复用 settings(group=models);插件经 Host.Invoke("models.catalog") 读取。
const MODELS_KEYS = ['models.catalog.claude', 'models.catalog.openai', 'models.catalog.kiro', 'models.catalog.seedance'] as const;
type ModelCatalogSettingKey = typeof MODELS_KEYS[number];
const MODEL_CATALOG_PLATFORMS: Array<{ key: ModelCatalogSettingKey; labelKey: string }> = [
  { key: 'models.catalog.claude', labelKey: 'settings.models_platform_claude' },
  { key: 'models.catalog.openai', labelKey: 'settings.models_platform_openai' },
  { key: 'models.catalog.kiro', labelKey: 'settings.models_platform_kiro' },
  { key: 'models.catalog.seedance', labelKey: 'settings.models_platform_seedance' },
];

// Seedance 视频桶价：分辨率 × 是否带参考图。桶键 = `${分辨率}_${no|with}_ref`。
// 与插件 registry / models.catalog.seedance overlay 的桶命名一致。
const SEEDANCE_RESOLUTIONS = ['480p', '720p', '1080p', '4k'] as const;
const SEEDANCE_REFS = [
  { suffix: 'no_ref', labelKey: 'settings.models_video_no_ref' },
  { suffix: 'with_ref', labelKey: 'settings.models_video_with_ref' },
] as const;
const SEEDANCE_BUCKETS: string[] = SEEDANCE_RESOLUTIONS.flatMap((res) => SEEDANCE_REFS.map((r) => `${res}_${r.suffix}`));
const SEEDANCE_TIERS = ['standard', 'fast', 'mini'] as const;

const OPENCLAW_KEYS = [
  'openclaw.enabled',
  'openclaw.provider_name',
  'openclaw.base_url',
  'openclaw.models_preset',
  'openclaw.memory_search_enabled',
  'openclaw.memory_search_model',
] as const;

const DEFAULT_OPENCLAW_PROVIDER_NAME = 'airgate';
const DEFAULT_OPENCLAW_MEMORY_MODEL = 'text-embedding-3-small';
const DEFAULT_OPENCLAW_MODELS_PRESET = `[
  {
    "id": "gpt-5.4",
    "label": "GPT-5.4 (推荐)",
    "api": "openai-responses",
    "reasoning": true,
    "input": ["text", "image"]
  },
  {
    "id": "claude-sonnet-4-6",
    "label": "Claude Sonnet 4.6",
    "api": "anthropic-messages",
    "reasoning": true,
    "input": ["text", "image"]
  },
  {
    "id": "claude-opus-4-6",
    "label": "Claude Opus 4.6",
    "api": "anthropic-messages",
    "reasoning": true,
    "input": ["text", "image"]
  }
]`;

// 官网价格表默认模板：与 landing/index.html 2026-07-03 的硬编码表同构。
// 单元格词汇见 landing/assets/pricing-render.js 注释；留空 = 官网使用页面内置硬编码表。
const DEFAULT_LANDING_PRICING_JSON = `{
  "tables": {
    "claude-max": [
      {"hl":true,"cells":[{"model":"claude-fable-5","tag":"旗舰","tagStyle":"pri"},{"strike":"¥68 / ¥340","note":"官方 $10 / $50"},{"deal":true,"strong":"¥21.30 / ¥106.50","note":"读 ¥2.13 · 写 ¥26.63 / ¥42.60"},{"pill":"省约 69%"},{"text":"最新高推理模型"}]},
      {"cells":[{"model":"claude-opus-4-8","tag":"主推","tagStyle":"pri"},{"strike":"¥34 / ¥170","note":"官方 $5 / $25"},{"deal":true,"strong":"¥10.65 / ¥53.25","note":"读 ¥1.065 · 写 ¥13.31 / ¥21.30"},{"pill":"省约 69%"},{"text":"最高推理，Claude Code 重任务"}]},
      {"cells":[{"model":"claude-opus-4-7"},{"strike":"¥34 / ¥170","note":"官方 $5 / $25"},{"deal":true,"strong":"¥10.65 / ¥53.25","note":"读 ¥1.065 · 写 ¥13.31 / ¥21.30"},{"pill":"省约 69%"},{"text":"1M 上下文 Opus"}]},
      {"cells":[{"model":"claude-opus-4-6"},{"strike":"¥34 / ¥170","note":"官方 $5 / $25"},{"deal":true,"strong":"¥10.65 / ¥53.25","note":"读 ¥1.065 · 写 ¥13.31 / ¥21.30"},{"pill":"省约 69%"},{"text":"1M 上下文 Opus"}]},
      {"cells":[{"model":"claude-opus-4-5-20251101"},{"strike":"¥34 / ¥170","note":"官方 $5 / $25"},{"deal":true,"strong":"¥10.65 / ¥53.25","note":"读 ¥1.065 · 写 ¥13.31 / ¥21.30"},{"pill":"省约 69%"},{"text":"兼容短名 claude-opus-4-5"}]},
      {"hl":true,"cells":[{"model":"claude-sonnet-5","tag":"新增","tagStyle":"key"},{"strike":"¥20.4 / ¥102","note":"官方 $3 / $15"},{"deal":true,"strong":"¥6.39 / ¥31.95","note":"读 ¥0.639 · 写 ¥7.99 / ¥12.78"},{"pill":"省约 69%"},{"text":"新一代 Sonnet 主力；官方引导价至 2026-08-31"}]},
      {"cells":[{"model":"claude-sonnet-4-6","tag":"均衡","tagStyle":"key"},{"strike":"¥20.4 / ¥102","note":"官方 $3 / $15"},{"deal":true,"strong":"¥6.39 / ¥31.95","note":"读 ¥0.639 · 写 ¥7.99 / ¥12.78"},{"pill":"省约 69%"},{"text":"均衡主力，适合长上下文"}]},
      {"cells":[{"model":"claude-sonnet-4-5-20250929"},{"strike":"¥20.4 / ¥102","note":"官方 $3 / $15"},{"deal":true,"strong":"¥6.39 / ¥31.95","note":"读 ¥0.639 · 写 ¥7.99 / ¥12.78"},{"pill":"省约 69%"},{"text":"兼容短名 claude-sonnet-4-5"}]},
      {"cells":[{"model":"claude-sonnet-4-20250514","tag":"已停用 / 自动转 4.6","tagStyle":"warn"},{"strike":"¥20.4 / ¥102","note":"官方 $3 / $15"},{"deal":true,"strong":"¥6.39 / ¥31.95","note":"读 ¥0.639 · 写 ¥7.99 / ¥12.78"},{"pill":"省约 69%"},{"text":"旧 ID 自动转 claude-sonnet-4-6"}]},
      {"cells":[{"model":"claude-haiku-4-5-20251001"},{"strike":"¥6.8 / ¥34","note":"官方 $1 / $5"},{"deal":true,"strong":"¥2.13 / ¥10.65","note":"读 ¥0.213 · 写 ¥2.66 / ¥4.26"},{"pill":"省约 69%"},{"text":"轻量低价模型"}]}
    ],
    "claude-aws": [
      {"cells":[{"model":"claude-opus-4-8","tag":"主推","tagStyle":"pri"},{"strike":"¥34 / ¥170","note":"官方 $5 / $25"},{"deal":true,"strong":"¥17.50 / ¥87.50","save":"约 5.1 折 · 省约 49%"},{"strong":"读 ¥1.75","note":"写 5m ¥21.88 / 1h ¥35.00"},{"text":"最高推理，适合 Claude Code 重任务"}]},
      {"cells":[{"model":"claude-opus-4-7 / 4-6"},{"strike":"¥34 / ¥170","note":"官方 $5 / $25"},{"deal":true,"strong":"¥17.50 / ¥87.50","save":"约 5.1 折 · 省约 49%"},{"strong":"读 ¥1.75","note":"写 5m ¥21.88 / 1h ¥35.00"},{"text":"Opus 系列，复杂规划与长任务"}]},
      {"cells":[{"model":"claude-opus-4-5-20251101"},{"strike":"¥34 / ¥170","note":"官方 $5 / $25"},{"deal":true,"strong":"¥17.50 / ¥87.50","save":"约 5.1 折 · 省约 49%"},{"strong":"读 ¥1.75","note":"写 5m ¥21.88 / 1h ¥35.00"},{"text":"兼容短名 claude-opus-4-5"}]}
    ],
    "claude-kiro": [
      {"hl":true,"cells":[{"model":"claude-fable-5","tag":"旗舰","tagStyle":"pri"},{"strike":"¥68 / ¥340","note":"官方 $10 / $50"},{"deal":true,"strong":"¥25.00 / ¥125.00","note":"读 ¥2.50 · 写 ¥31.25 / ¥50.00"},{"pill":"约 3.7 折 · 省约 63%"},{"text":"最新高推理模型"}]},
      {"cells":[{"model":"claude-opus-4-8","tag":"主推","tagStyle":"pri"},{"strike":"¥34 / ¥170","note":"官方 $5 / $25"},{"deal":true,"strong":"¥12.50 / ¥62.50","note":"读 ¥1.25 · 写 ¥15.63 / ¥25.00"},{"pill":"约 3.7 折 · 省约 63%"},{"text":"最高推理，Claude Code 重任务"}]},
      {"cells":[{"model":"claude-opus-4-7 / 4-6"},{"strike":"¥34 / ¥170","note":"官方 $5 / $25"},{"deal":true,"strong":"¥12.50 / ¥62.50","note":"读 ¥1.25 · 写 ¥15.63 / ¥25.00"},{"pill":"约 3.7 折 · 省约 63%"},{"text":"1M 上下文 Opus"}]},
      {"cells":[{"model":"claude-opus-4-5-20251101"},{"strike":"¥34 / ¥170","note":"官方 $5 / $25"},{"deal":true,"strong":"¥12.50 / ¥62.50","note":"读 ¥1.25 · 写 ¥15.63 / ¥25.00"},{"pill":"约 3.7 折 · 省约 63%"},{"text":"兼容短名 claude-opus-4-5"}]},
      {"hl":true,"cells":[{"model":"claude-sonnet-5","tag":"新增","tagStyle":"key"},{"strike":"¥20.4 / ¥102","note":"官方 $3 / $15"},{"deal":true,"strong":"¥7.50 / ¥37.50","note":"读 ¥0.75 · 写 ¥9.38 / ¥15.00"},{"pill":"约 3.7 折 · 省约 63%"},{"text":"新一代 Sonnet 主力；官方引导价至 2026-08-31"}]},
      {"cells":[{"model":"claude-sonnet-4-6","tag":"均衡","tagStyle":"key"},{"strike":"¥20.4 / ¥102","note":"官方 $3 / $15"},{"deal":true,"strong":"¥7.50 / ¥37.50","note":"读 ¥0.75 · 写 ¥9.38 / ¥15.00"},{"pill":"约 3.7 折 · 省约 63%"},{"text":"均衡主力，适合长上下文"}]},
      {"cells":[{"model":"claude-sonnet-4-5-20250929"},{"strike":"¥20.4 / ¥102","note":"官方 $3 / $15"},{"deal":true,"strong":"¥7.50 / ¥37.50","note":"读 ¥0.75 · 写 ¥9.38 / ¥15.00"},{"pill":"约 3.7 折 · 省约 63%"},{"text":"兼容短名 claude-sonnet-4-5"}]},
      {"cells":[{"model":"claude-sonnet-4-20250514","tag":"已停用 / 自动转 4.6","tagStyle":"warn"},{"strike":"¥20.4 / ¥102","note":"官方 $3 / $15"},{"deal":true,"strong":"¥7.50 / ¥37.50","note":"读 ¥0.75 · 写 ¥9.38 / ¥15.00"},{"pill":"约 3.7 折 · 省约 63%"},{"text":"旧 ID 自动转 claude-sonnet-4-6"}]},
      {"cells":[{"model":"claude-haiku-4-5-20251001"},{"strike":"¥6.8 / ¥34","note":"官方 $1 / $5"},{"deal":true,"strong":"¥2.50 / ¥12.50","note":"读 ¥0.25 · 写 ¥3.13 / ¥5.00"},{"pill":"约 3.7 折 · 省约 63%"},{"text":"轻量低价模型"}]}
    ],
    "openai": [
      {"hl":true,"cells":[{"model":"gpt-5.5","tag":"主力","tagStyle":"key"},{"strike":"¥34 / ¥204","note":"官方 $5 / $30 · 缓存 $0.5"},{"deal":true,"strong":"¥2.25 / ¥13.50","save":"约 0.66 折 · 缓存 ¥0.225"},{"strong":"¥3.00 / ¥18.00","note":"约 0.88 折 · 缓存 ¥0.30"},{"text":"复杂推理与 Codex 主力模型"}]},
      {"cells":[{"model":"gpt-5.4"},{"strike":"¥17 / ¥102","note":"官方 $2.5 / $15 · 缓存 $0.25"},{"deal":true,"strong":"¥1.125 / ¥6.75","save":"约 0.66 折 · 缓存 ¥0.113"},{"strong":"¥1.50 / ¥9.00","note":"约 0.88 折 · 缓存 ¥0.15"},{"text":"272K 以上长上下文有阶梯倍率"}]},
      {"cells":[{"model":"gpt-5.4-mini"},{"strike":"¥5.1 / ¥30.6","note":"官方 $0.75 / $4.5 · 缓存 $0.075"},{"deal":true,"strong":"¥0.338 / ¥2.025","save":"约 0.66 折 · 缓存 ¥0.034"},{"strong":"¥0.45 / ¥2.70","note":"约 0.88 折 · 缓存 ¥0.045"},{"text":"低延迟低成本"}]},
      {"cells":[{"model":"gpt-5.3-codex-spark"},{"strike":"¥11.9 / ¥95.2","note":"官方 $1.75 / $14 · 缓存 $0.175"},{"deal":true,"strong":"¥0.787 / ¥6.30","save":"约 0.66 折 · 缓存 ¥0.079"},{"strong":"¥1.05 / ¥8.40","note":"约 0.88 折 · 缓存 ¥0.105"},{"text":"Codex 轻量任务"}]},
      {"cells":[{"model":"gpt-image-1 / 1.5 / 2","tag":"图像","tagStyle":"img"},{"strike":"¥34 / ¥204","note":"官方 $5 / $30 · 缓存 $0.5"},{"deal":true,"strong":"¥2.25 / ¥13.50","save":"约 0.66 折 · 缓存 ¥0.225"},{"strong":"¥3.00 / ¥18.00","note":"约 0.88 折 · 缓存 ¥0.30"},{"text":"图像接口 Token 计费；固定图价见「图像生成」"}]}
    ]
  },
  "panels": [
    {
      "key": "glm",
      "tab": "GLM",
      "insertBefore": "image",
      "title": "GLM 模型",
      "tag": "OpenAI 兼容",
      "tagStyle": "key",
      "lead": "GLM 5.2 通过 OpenAI 兼容协议接入，按官方直付价 5.5 折结算（每官方 $1 扣 ¥3.74），适合中文推理、长上下文和高并发文本任务。",
      "minWidth": "900px",
      "headers": ["模型 ID", "官方直付价（$）", {"deal":true,"badge":"新品特惠","text":"GLM 专属通道"}, "折扣", "说明"],
      "rows": [
        {"hl":true,"cells":[{"model":"glm-5.2","tag":"1M 上下文","tagStyle":"key"},{"strike":"$1.40 / $4.40","note":"z.ai 官方牌价 · 缓存 $0.26"},{"deal":true,"strong":"$0.77 / $2.42","save":"5.5 折 · 缓存 $0.14"},{"pill":"省约 45%"},{"text":"OpenAI 兼容 /v1/chat/completions"}]}
      ],
      "units": "› 价格单位：美元 / 百万 Token（输入 / 输出）。缓存为 Prompt Cache 命中读取价；折扣 = 实付价 ÷ 官方直付价（按 ¥6.8/$ 参考汇率，以输入价计）；价格会因汇率和渠道成本波动略有差异，实际可用模型与扣费以控制台为准。"
    }
  ]
}`;

const DEFAULT_EMAIL_SUBJECT = '{{site_name}} - 邮箱验证码';
const DEFAULT_EMAIL_BODY = `<div style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; max-width: 420px; margin: 0 auto; background: #ffffff; border-radius: 8px; border: 1px solid #e5e7eb;">
  <div style="padding: 32px 28px;">
    <div style="font-size: 16px; font-weight: 600; color: #111; margin-bottom: 20px;">{{site_name}}</div>
    <p style="color: #555; font-size: 14px; line-height: 1.6; margin: 0 0 24px;">您好，您正在注册账户，请使用以下验证码完成操作：</p>
    <div style="background: #f7f8fa; border: 1px solid #eef0f3; border-radius: 8px; padding: 20px; text-align: center; margin-bottom: 24px;">
      <span style="font-size: 32px; font-weight: 700; letter-spacing: 10px; color: #111;">{{code}}</span>
    </div>
    <p style="color: #999; font-size: 12px; line-height: 1.6; margin: 0;">验证码 10 分钟内有效，请勿泄露给他人。如非本人操作，请忽略此邮件。</p>
  </div>
  <div style="border-top: 1px solid #f0f0f0; padding: 14px 28px;">
    <p style="color: #c0c0c0; font-size: 11px; margin: 0; text-align: center;">此邮件由 {{site_name}} 系统自动发送，请勿直接回复</p>
  </div>
</div>`;

const DEFAULT_BALANCE_ALERT_SUBJECT = '{{site_name}} - 余额预警';
const DEFAULT_BALANCE_ALERT_BODY = `<div style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; max-width: 420px; margin: 0 auto; background: #ffffff; border-radius: 8px; border: 1px solid #e5e7eb;">
  <div style="padding: 32px 28px;">
    <div style="font-size: 16px; font-weight: 600; color: #111; margin-bottom: 20px;">{{site_name}}</div>
    <p style="color: #555; font-size: 14px; line-height: 1.6; margin: 0 0 16px;">您的账户余额已低于预警阈值：</p>
    <div style="background: #fef3c7; border: 1px solid #fde68a; border-radius: 8px; padding: 16px; margin-bottom: 20px;">
      <div style="display: flex; justify-content: space-between; margin-bottom: 8px;">
        <span style="color: #92400e; font-size: 13px;">当前余额</span>
        <span style="color: #92400e; font-size: 16px; font-weight: 700;">{{balance}}</span>
      </div>
      <div style="display: flex; justify-content: space-between;">
        <span style="color: #92400e; font-size: 13px;">预警阈值</span>
        <span style="color: #92400e; font-size: 13px;">{{threshold}}</span>
      </div>
    </div>
    <p style="color: #999; font-size: 12px; line-height: 1.6; margin: 0;">请及时充值以免影响正常使用。余额回到阈值以上后，预警将自动重置。</p>
  </div>
  <div style="border-top: 1px solid #f0f0f0; padding: 14px 28px;">
    <p style="color: #c0c0c0; font-size: 11px; margin: 0; text-align: center;">此邮件由 {{site_name}} 系统自动发送</p>
  </div>
</div>`;

// ==================== Tab 定义 ====================

type TabKey = 'site' | 'security' | 'smtp' | 'storage' | 'models' | 'openclaw' | 'system';

const TABS: { key: TabKey; labelKey: string; icon: typeof Globe }[] = [
  { key: 'site', labelKey: 'settings.tab_site', icon: Globe },
  { key: 'security', labelKey: 'settings.tab_security', icon: ShieldCheck },
  { key: 'smtp', labelKey: 'settings.tab_smtp', icon: Mail },
  { key: 'storage', labelKey: 'settings.tab_storage', icon: Database },
  { key: 'models', labelKey: 'settings.tab_models', icon: Boxes },
  { key: 'openclaw', labelKey: 'settings.tab_openclaw', icon: Zap },
  { key: 'system', labelKey: 'settings.tab_system', icon: Download },
];

// system tab 通过独立的 upgrade API 管理，不走通用 settings save 流程。
type SaveTabKey = Exclude<TabKey, 'security' | 'system'>;

const TAB_GROUP: Record<SaveTabKey, string> = {
  site: 'site',
  smtp: 'smtp',
  storage: 'storage',
  models: 'models',
  openclaw: 'openclaw',
};

const TAB_KEYS: Record<SaveTabKey, readonly string[]> = {
  site: SITE_KEYS,
  smtp: SMTP_KEYS,
  storage: STORAGE_KEYS,
  models: MODELS_KEYS,
  openclaw: OPENCLAW_KEYS,
};

// ==================== Component ====================

export default function SettingsPage() {
  const { t } = useTranslation();
  const { toast } = useToast();
  const queryClient = useQueryClient();

  const [activeTab, setActiveTab] = useState<TabKey>('site');
  const [values, setValues] = useState<Record<string, string>>({});
  const [hasChanges, setHasChanges] = useState(false);
  const [emailTplType, setEmailTplType] = useState<'verify' | 'balance_alert'>('verify');
  const [isEmailPreviewOpen, setEmailPreviewOpen] = useState(false);
  const [isSmtpTestOpen, setSmtpTestOpen] = useState(false);
  const [modelCatalogErrors, setModelCatalogErrors] = useState<Record<string, string[]>>({});

  // 获取所有设置
  const { data: settings, isLoading } = useQuery({
    queryKey: queryKeys.settings(),
    queryFn: () => settingsApi.list(),
  });

  // 初始化
  useEffect(() => {
    if (settings) {
      const map: Record<string, string> = {};
      for (const s of settings) {
        map[s.key] = s.value;
      }
      setValues(map);
      setHasChanges(false);
    }
  }, [settings]);

  // 保存
  const saveMutation = useCrudMutation({
    mutationFn: (items: SettingItem[]) => settingsApi.update({ settings: items }),
    successMessage: t('settings.save_success'),
    queryKey: queryKeys.settings(),
    onSuccess: () => {
      setHasChanges(false);
      queryClient.invalidateQueries({ queryKey: queryKeys.siteSettings() });
    },
  });

  // SMTP 测试
  const smtpTestMutation = useMutation({
    mutationFn: (data: TestSMTPReq) => settingsApi.testSMTP(data),
    onSuccess: () => {
      setSmtpTestOpen(false);
      toast('success', t('settings.smtp_test_success'));
    },
    onError: (err: Error) => toast('error', err.message),
  });

  function set(key: string, value: string) {
    setValues((prev) => ({ ...prev, [key]: value }));
    setHasChanges(true);
  }

  function val(key: string): string {
    return values[key] ?? '';
  }

  function boolVal(key: string): boolean {
    return val(key) === 'true';
  }

  const handleModelCatalogValidationChange = useCallback((key: ModelCatalogSettingKey, errors: string[]) => {
    setModelCatalogErrors((prev) => {
      const prevErrors = prev[key] ?? [];
      if (prevErrors.join('\n') === errors.join('\n')) return prev;
      return { ...prev, [key]: errors };
    });
  }, []);

  function buildSaveItems(): SettingItem[] {
    if (activeTab === 'system') return [];
    if (activeTab === 'security') {
      return [
        ...REG_KEYS.map((key) => ({
          key,
          value: values[key] ?? '',
          group: 'registration',
        })),
        ...DEFAULT_KEYS.map((key) => ({
          key,
          value: values[key] ?? '',
          group: 'defaults',
        })),
        ...OAUTH_KEYS.map((key) => ({
          key,
          value: values[key] ?? '',
          group: 'oauth',
        })),
      ];
    }

    const tab = activeTab as SaveTabKey;
    const group = TAB_GROUP[tab];
    const keys = TAB_KEYS[tab];
    return keys.map((key) => ({
      key,
      value: values[key] ?? '',
      group,
    }));
  }

  const hasModelCatalogErrors = Object.values(modelCatalogErrors).some((errs) => errs.length > 0);

  function handleSave() {
    if (activeTab === 'models' && hasModelCatalogErrors) {
      toast('error', t('settings.models_fix_errors'));
      return;
    }
    const items = buildSaveItems();
    if (items.length === 0) return;
    saveMutation.mutate(items);
  }

  function handleTestSMTP() {
    setSmtpTestOpen(true);
  }

  function submitSmtpTest(testTo: string) {
    smtpTestMutation.mutate({
      host: val('smtp_host'),
      port: Number(val('smtp_port')) || 587,
      username: val('smtp_username'),
      password: val('smtp_password'),
      use_tls: boolVal('smtp_use_tls'),
      from: val('smtp_from_email'),
      to: testTo,
    });
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-16">
        <Loader2 className="w-5 h-5 animate-spin text-primary" />
        <span className="ml-2 text-sm text-text-tertiary">{t('common.loading')}</span>
      </div>
    );
  }

  function renderSaveAction(left?: React.ReactNode) {
    if (activeTab === 'system') return null;
    return (
      <div className="ag-settings-card-footer">
        {left ? <div className="ag-settings-card-footer-left">{left}</div> : null}
        <Button
          onPress={handleSave}
          isDisabled={!hasChanges || saveMutation.isPending || (activeTab === 'models' && hasModelCatalogErrors)}
          aria-busy={saveMutation.isPending}
        >
          <Save className="w-4 h-4" />
          {t('common.save')}
        </Button>
      </div>
    );
  }

  const saveAction = renderSaveAction();
  const smtpSaveAction = renderSaveAction(
    <>
      <Button
        size="sm"
        variant="ghost"
        onPress={() => setEmailPreviewOpen(true)}
      >
        <MailSearch className="w-3.5 h-3.5" />
        {t('settings.template_preview')}
      </Button>
      <Button
        size="sm"
        variant="ghost"
        onPress={() => {
          if (emailTplType === 'verify') {
            set('email_template_subject', DEFAULT_EMAIL_SUBJECT);
            set('email_template_body', DEFAULT_EMAIL_BODY);
            return;
          }
          set('balance_alert_email_subject', DEFAULT_BALANCE_ALERT_SUBJECT);
          set('balance_alert_email_body', DEFAULT_BALANCE_ALERT_BODY);
        }}
      >
        <RotateCcw className="w-3.5 h-3.5" />
        {t('settings.template_reset')}
      </Button>
    </>,
  );

  // 官网价格表 JSON 的客户端校验：只提示不阻塞保存（留空 = 官网使用内置硬编码表）。
  const landingPricingRaw = values['landing_pricing_json'] ?? '';
  let landingPricingError = '';
  if (landingPricingRaw.trim() !== '') {
    try {
      const parsed = JSON.parse(landingPricingRaw);
      // 注意：typeof null === 'object'，不能只用 typeof 判断，否则 {"tables": null}
      // 会被判定为合法（保存后 pricing-render.js 静默保留旧的硬编码表，管理员毫无提示）。
      const hasTables = parsed && typeof parsed === 'object' && !Array.isArray(parsed) &&
        parsed.tables && typeof parsed.tables === 'object' && !Array.isArray(parsed.tables);
      const hasPanels = parsed && typeof parsed === 'object' && !Array.isArray(parsed) &&
        Array.isArray(parsed.panels);
      if (!hasTables && !hasPanels) {
        landingPricingError = t('settings.landing_pricing_invalid');
      }
    } catch (e) {
      landingPricingError = (e as Error).message;
    }
  }

  // 多落地页品牌 JSON 的客户端校验：只提示不阻塞保存。
  // 留空 = 所有来源统一用全局 site_name/site_logo/doc_url。
  const sitesBrandingRaw = values['sites_branding'] ?? '';
  let sitesBrandingError = '';
  if (sitesBrandingRaw.trim() !== '') {
    try {
      const parsed = JSON.parse(sitesBrandingRaw);
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
        sitesBrandingError = t('settings.sites_branding_invalid');
      }
    } catch (e) {
      sitesBrandingError = (e as Error).message;
    }
  }

  return (
    <div className="max-w-6xl mx-auto w-full px-4 sm:px-6 lg:px-8 py-6 md:py-8 flex flex-col gap-6 min-h-screen">
      <div className="mx-auto w-full max-w-full overflow-x-auto hide-scrollbar pb-1">
        <Tabs
          className="ag-page-tabs ag-settings-tabs whitespace-nowrap"
          selectedKey={activeTab}
          onSelectionChange={(key) => setActiveTab(key as TabKey)}
        >
          <Tabs.List>
            {TABS.map((tab, index) => {
              const Icon = tab.icon;
              return (
                <Tabs.Tab key={tab.key} id={tab.key}>
                  {index > 0 ? <Tabs.Separator /> : null}
                  <Tabs.Indicator />
                  <Icon className="w-4 h-4" />
                  <span>{t(tab.labelKey)}</span>
                </Tabs.Tab>
              );
            })}
          </Tabs.List>
        </Tabs>
      </div>

      {/* Content */}
      <div className="flex-1 w-full flex flex-col gap-6">
        {activeTab === 'site' && (
          <Card>
            <Card.Header>
              <Card.Title>{t('settings.site_branding')}</Card.Title>
            </Card.Header>
            <Card.Content>
              <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                <Field label={t('settings.site_name')} hint={t('settings.site_name_hint')}>
                  <Input value={val('site_name')} onChange={(e) => set('site_name', e.target.value)} placeholder="HopBase" />
                </Field>
                <Field label={t('settings.site_subtitle')}>
                  <Input value={val('site_subtitle')} onChange={(e) => set('site_subtitle', e.target.value)} placeholder="AI API Gateway" />
                </Field>
                <Field className="col-span-1 md:col-span-2" label={t('settings.api_base_url')} hint={t('settings.api_base_url_hint')}>
                  <Input value={val('api_base_url')} onChange={(e) => set('api_base_url', e.target.value)} placeholder="https://api.example.com" />
                </Field>
                <Field label={t('settings.contact_info')}>
                  <Input value={val('contact_info')} onChange={(e) => set('contact_info', e.target.value)} />
                </Field>
                <Field label={t('settings.doc_url')}>
                  <Input value={val('doc_url')} onChange={(e) => set('doc_url', e.target.value)} placeholder="https://docs.example.com" />
                </Field>
                <Field className="col-span-1 md:col-span-2" label={t('settings.site_logo')} hint={t('settings.site_logo_hint')}>
                  <LogoUpload value={val('site_logo')} onChange={(url) => set('site_logo', url)} />
                </Field>
              </div>
              <div className="ag-settings-section-stack mt-6">
                <SettingsSection
                  action={(
                    <Button
                      size="sm"
                      variant="ghost"
                      onPress={() => set('landing_pricing_json', DEFAULT_LANDING_PRICING_JSON)}
                    >
                      <RotateCcw className="w-3.5 h-3.5" />
                      {t('settings.template_reset')}
                    </Button>
                  )}
                  description={t('settings.landing_pricing_desc')}
                  title={t('settings.landing_pricing')}
                >
                  <TextArea
                    aria-label={t('settings.landing_pricing')}
                    value={landingPricingRaw}
                    onChange={(e) => set('landing_pricing_json', e.target.value)}
                    className="h-80 w-full font-mono text-xs leading-5"
                    placeholder={DEFAULT_LANDING_PRICING_JSON}
                  />
                  {landingPricingError && (
                    <p className="text-[11px] text-danger mt-1.5">{landingPricingError}</p>
                  )}
                </SettingsSection>

                <SettingsSection
                  description={t('settings.sites_branding_desc')}
                  title={t('settings.sites_branding')}
                >
                  <TextArea
                    aria-label={t('settings.sites_branding')}
                    value={sitesBrandingRaw}
                    onChange={(e) => set('sites_branding', e.target.value)}
                    className="h-40 w-full font-mono text-xs leading-5"
                    placeholder={'{\n  "ink": { "name": "Essevin", "logo": "https://essevin.com/logo.svg", "doc_url": "https://essevin.com/docs" },\n  "open-late": { "name": "Essevin", "logo": "https://late.essevin.com/logo.svg", "doc_url": "https://late.essevin.com/docs" },\n  "kite": { "name": "KITE", "logo": "data:image/svg+xml;base64,...", "doc_url": "https://kite.essevin.com/docs" }\n}'}
                  />
                  {sitesBrandingError && (
                    <p className="text-[11px] text-danger mt-1.5">{sitesBrandingError}</p>
                  )}
                </SettingsSection>

                <SettingsSection
                  description={t('settings.announcement_desc')}
                  title={t('settings.announcement_title')}
                >
                  <div className="space-y-4">
                    <NativeSwitch
                      isSelected={boolVal('announcement_enabled')}
                      label={<span className="text-sm font-medium text-text">{t('settings.announcement_enabled')}</span>}
                      onChange={(v) => set('announcement_enabled', String(v))}
                    />
                    <Field label={t('settings.announcement_level')}>
                      <div className="flex max-w-md gap-1">
                        {(['info', 'warning', 'danger'] as const).map((lv) => (
                          <Button
                            key={lv}
                            fullWidth
                            size="sm"
                            variant={(val('announcement_level') || 'info') === lv ? 'primary' : 'secondary'}
                            onPress={() => set('announcement_level', lv)}
                          >
                            {t(`settings.announcement_level_${lv}`)}
                          </Button>
                        ))}
                      </div>
                    </Field>
                    <Field label={t('settings.announcement_content')}>
                      <TextArea
                        aria-label={t('settings.announcement_content')}
                        value={val('announcement_content')}
                        onChange={(e) => set('announcement_content', e.target.value)}
                        className="h-24 w-full text-sm leading-5"
                        placeholder={t('settings.announcement_content_ph')}
                      />
                    </Field>
                  </div>
                </SettingsSection>
              </div>
              {saveAction}
            </Card.Content>
          </Card>
        )}

        {activeTab === 'security' && (
          <Card>
            <Card.Header>
              <Card.Title>{t('settings.tab_security')}</Card.Title>
            </Card.Header>
            <Card.Content>
              <div className="ag-settings-section-stack">
                <SecurityPanel />

                <SettingsSection title={t('settings.registration_auth')}>
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                    <div className="space-y-6">
                      <NativeSwitch
                        isSelected={boolVal('registration_enabled')}
                        label={(
                          <>
                            <span className="text-sm font-medium text-text">{t('settings.registration_enabled')}</span>
                            <span className="block text-xs text-text-tertiary">{t('settings.registration_enabled_desc')}</span>
                          </>
                        )}
                        onChange={(v) => set('registration_enabled', String(v))}
                      />
                      <NativeSwitch
                        isDisabled={!val('smtp_host')}
                        isSelected={boolVal('email_verify_enabled')}
                        label={(
                          <>
                            <span className="text-sm font-medium text-text">{t('settings.email_verify_enabled')}</span>
                            <span className="block text-xs text-text-tertiary">
                              {val('smtp_host') ? t('settings.email_verify_enabled_desc') : t('settings.email_verify_no_smtp')}
                            </span>
                          </>
                        )}
                        onChange={(v) => {
                          if (v && !val('smtp_host')) return;
                          set('email_verify_enabled', String(v));
                        }}
                      />
                    </div>
                    <Field className="col-span-1" label={t('settings.email_suffix_whitelist')} hint={t('settings.email_suffix_whitelist_hint')}>
                    <TextArea
                        value={val('registration_email_suffix_whitelist')}
                        onChange={(e) => set('registration_email_suffix_whitelist', e.target.value)}
                        rows={3}
                        placeholder="gmail.com&#10;outlook.com"
                      />
                    </Field>
                  </div>
                </SettingsSection>

                <SettingsSection title={t('settings.oauth_section', { defaultValue: '第三方登录' })}>
                  <div className="space-y-6">
                    {OAUTH_PROVIDERS.map(({ id, label }) => (
                      <div key={id} className="rounded-lg border border-glass-border p-4 space-y-4">
                        <NativeSwitch
                          isSelected={boolVal(`oauth_${id}_enabled`)}
                          label={(
                            <>
                              <span className="text-sm font-medium text-text">
                                {t('settings.oauth_provider_enabled', { defaultValue: '启用 {{provider}} 登录', provider: label })}
                              </span>
                              <span className="block text-xs text-text-tertiary">
                                {t('settings.oauth_provider_enabled_desc', { defaultValue: '需先在 {{provider}} 侧创建 OAuth 应用并填入下方凭证', provider: label })}
                              </span>
                            </>
                          )}
                          onChange={(v) => set(`oauth_${id}_enabled`, String(v))}
                        />
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                          <Field label="Client ID">
                            <Input
                              value={val(`oauth_${id}_client_id`)}
                              onChange={(e) => set(`oauth_${id}_client_id`, e.target.value)}
                              placeholder={id === 'google' ? 'xxx.apps.googleusercontent.com' : 'Ov23...'}
                              autoComplete="off"
                            />
                          </Field>
                          <Field label="Client Secret">
                            <Input
                              type="password"
                              value={val(`oauth_${id}_client_secret`)}
                              onChange={(e) => set(`oauth_${id}_client_secret`, e.target.value)}
                              placeholder="••••••••"
                              autoComplete="new-password"
                            />
                          </Field>
                        </div>
                        <p className="text-xs text-text-tertiary break-all">
                          {t('settings.oauth_callback_hint', { defaultValue: '回调地址（需与平台侧登记完全一致）：' })}
                          <code className="ml-1 font-mono">
                            {`${(val('api_base_url') || 'https://<api-domain>').replace(/\/+$/, '')}/api/v1/auth/oauth/${id}/callback`}
                          </code>
                        </p>
                      </div>
                    ))}
                  </div>
                </SettingsSection>

                <SettingsSection title={t('settings.new_user_defaults')}>
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                    <Field label={t('settings.default_balance')} hint={t('settings.default_balance_hint')}>
                      <Input
                        type="number"
                        value={val('default_balance')}
                        onChange={(e) => set('default_balance', e.target.value)}
                        placeholder="0"
                      />
                    </Field>
                    <Field label={t('settings.default_concurrency')} hint={t('settings.default_concurrency_hint')}>
                      <Input
                        type="number"
                        value={val('default_concurrency')}
                        onChange={(e) => set('default_concurrency', e.target.value)}
                        placeholder="5"
                      />
                    </Field>
                  </div>
                </SettingsSection>
              </div>
              {saveAction}
            </Card.Content>
          </Card>
        )}

        {activeTab === 'smtp' && (
          <Card>
            <Card.Header className="justify-between gap-3">
              <Card.Title>{t('settings.smtp_config')}</Card.Title>
              <Button
                size="sm"
                variant="secondary"
                onPress={handleTestSMTP}
                isDisabled={!val('smtp_host') || smtpTestMutation.isPending}
                aria-busy={smtpTestMutation.isPending}
              >
                <Send className="w-3.5 h-3.5" />
                {t('settings.smtp_test')}
              </Button>
            </Card.Header>
            <Card.Content>
              <div className="ag-settings-section-stack">
                <SettingsSection title={t('settings.smtp_config')}>
                  <Form className="space-y-4" onSubmit={(e) => e.preventDefault()}>
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                      <Field label={t('settings.smtp_host')}>
                        <Input value={val('smtp_host')} onChange={(e) => set('smtp_host', e.target.value)} placeholder="smtp.gmail.com" />
                      </Field>
                      <Field label={t('settings.smtp_port')}>
                        <Input type="number" value={val('smtp_port')} onChange={(e) => set('smtp_port', e.target.value)} placeholder="587" />
                      </Field>
                    </div>
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                      <Field label={t('settings.smtp_username')}>
                        <Input value={val('smtp_username')} onChange={(e) => set('smtp_username', e.target.value)} />
                      </Field>
                      <Field label={t('settings.smtp_password')}>
                        <Input name="smtp_password" type="password" value={val('smtp_password')} onChange={(e) => set('smtp_password', e.target.value)} autoComplete="off" />
                      </Field>
                    </div>
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                      <Field label={t('settings.smtp_from_email')}>
                        <Input value={val('smtp_from_email')} onChange={(e) => set('smtp_from_email', e.target.value)} placeholder="noreply@example.com" />
                      </Field>
                      <Field label={t('settings.smtp_from_name')}>
                        <Input value={val('smtp_from_name')} onChange={(e) => set('smtp_from_name', e.target.value)} placeholder="HopBase" />
                      </Field>
                    </div>
                    <NativeSwitch
                      isSelected={boolVal('smtp_use_tls')}
                      label={(
                        <>
                          <span className="text-sm font-medium text-text">{t('settings.smtp_use_tls')}</span>
                          <span className="block text-xs text-text-tertiary">{t('settings.smtp_use_tls_desc')}</span>
                        </>
                      )}
                      onChange={(v) => set('smtp_use_tls', String(v))}
                    />
                  </Form>
                </SettingsSection>

                <SettingsSection
                  action={(
                    <Tabs
                      className="ag-page-tabs ag-page-tabs-compact"
                      selectedKey={emailTplType}
                      onSelectionChange={(key) => setEmailTplType(key as 'verify' | 'balance_alert')}
                    >
                      <Tabs.List>
                        <Tabs.Tab id="verify">
                          <Tabs.Indicator />
                          <span>{t('settings.email_template')}</span>
                        </Tabs.Tab>
                        <Tabs.Tab id="balance_alert">
                          <Tabs.Separator />
                          <Tabs.Indicator />
                          <span>{t('settings.balance_alert_email_template')}</span>
                        </Tabs.Tab>
                      </Tabs.List>
                    </Tabs>
                  )}
                  title={emailTplType === 'verify' ? t('settings.email_template') : t('settings.balance_alert_email_template')}
                >
                  {emailTplType === 'verify' ? (
                    <EmailTemplateEditor
                      subject={val('email_template_subject') || DEFAULT_EMAIL_SUBJECT}
                      body={val('email_template_body') || DEFAULT_EMAIL_BODY}
                      onSubjectChange={(v) => set('email_template_subject', v)}
                      onBodyChange={(v) => set('email_template_body', v)}
                      siteName={val('site_name') || 'HopBase'}
                      variables={[
                        { name: 'site_name', sample: val('site_name') || 'HopBase' },
                        { name: 'code', sample: '888888' },
                        { name: 'email', sample: 'user@example.com' },
                      ]}
                      isPreviewOpen={isEmailPreviewOpen}
                      onPreviewOpenChange={setEmailPreviewOpen}
                    />
                  ) : (
                    <EmailTemplateEditor
                      subject={val('balance_alert_email_subject') || DEFAULT_BALANCE_ALERT_SUBJECT}
                      body={val('balance_alert_email_body') || DEFAULT_BALANCE_ALERT_BODY}
                      onSubjectChange={(v) => set('balance_alert_email_subject', v)}
                      onBodyChange={(v) => set('balance_alert_email_body', v)}
                      siteName={val('site_name') || 'HopBase'}
                      variables={[
                        { name: 'site_name', sample: val('site_name') || 'HopBase' },
                        { name: 'balance', sample: '$1.2345' },
                        { name: 'threshold', sample: '$5.00' },
                      ]}
                      isPreviewOpen={isEmailPreviewOpen}
                      onPreviewOpenChange={setEmailPreviewOpen}
                    />
                  )}
                </SettingsSection>
              </div>
              {smtpSaveAction}
            </Card.Content>
          </Card>
        )}

        {activeTab === 'storage' && (
          <StoragePanel set={set} boolVal={boolVal} val={val} footer={saveAction} />
        )}

        {activeTab === 'models' && (
          <ModelCatalogPanel
            values={values}
            set={set}
            footer={saveAction}
            onValidationChange={handleModelCatalogValidationChange}
          />
        )}

        {activeTab === 'openclaw' && (
          <OpenClawPanel
            values={values}
            set={set}
            boolVal={boolVal}
            val={val}
            footer={saveAction}
          />
        )}

        {activeTab === 'system' && <SystemUpdatePanel />}
      </div>

      <SmtpTestModal
        isPending={smtpTestMutation.isPending}
        open={isSmtpTestOpen}
        onClose={() => setSmtpTestOpen(false)}
        onSubmit={submitSmtpTest}
      />
    </div>
  );
}

// ==================== SMTP Test Modal ====================

function SmtpTestModal({
  isPending,
  onClose,
  onSubmit,
  open,
}: {
  isPending: boolean;
  onClose: () => void;
  onSubmit: (email: string) => void;
  open: boolean;
}) {
  const { t } = useTranslation();
  const [email, setEmail] = useState('');
  const trimmedEmail = email.trim();

  useEffect(() => {
    if (open) setEmail('');
  }, [open]);

  const modalState = useOverlayState({
    isOpen: open,
    onOpenChange: (nextOpen) => {
      if (!nextOpen && !isPending) onClose();
    },
  });

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!trimmedEmail || isPending) return;
    onSubmit(trimmedEmail);
  }

  return (
    <CommonModal
      description={t('settings.smtp_test_prompt')}
      footer={(
        <div className="flex w-full justify-end gap-2">
          <Button variant="secondary" onPress={onClose} isDisabled={isPending}>
            {t('common.cancel')}
          </Button>
          <Button
            aria-busy={isPending}
            form="smtp-test-form"
            isDisabled={isPending || !trimmedEmail}
            type="submit"
            variant="primary"
          >
            {isPending ? <Spinner size="sm" /> : null}
            {t('settings.smtp_test')}
          </Button>
        </div>
      )}
      icon={<Send className="w-4 h-4" />}
      showCloseTrigger={!isPending}
      size="sm"
      state={modalState}
      surface={false}
      title={t('settings.smtp_test')}
    >
      <Form id="smtp-test-form" className="space-y-4" onSubmit={handleSubmit}>
        <Field label={t('settings.smtp_test_recipient')}>
          <Input
            autoFocus
            autoComplete="email"
            disabled={isPending}
            onChange={(event) => setEmail(event.target.value)}
            placeholder="user@example.com"
            type="text"
            value={email}
          />
        </Field>
      </Form>
    </CommonModal>
  );
}

// ==================== Security Panel ====================

function SecurityPanel() {
  const { t } = useTranslation();
  const { toast } = useToast();
  const queryClient = useQueryClient();
  const copy = useClipboard();

  const [showKeyModal, setShowKeyModal] = useState(false);
  const [plainKey, setPlainKey] = useState('');
  const [confirmRegen, setConfirmRegen] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);

  const { data, isLoading } = useQuery({
    queryKey: queryKeys.adminApiKey(),
    queryFn: () => adminApiKeyApi.get(),
  });

  const hasKey = !!data?.hint;

  const generateMutation = useMutation({
    mutationFn: () => adminApiKeyApi.generate(),
    onSuccess: (resp: AdminAPIKeyResp) => {
      queryClient.setQueryData(queryKeys.adminApiKey(), { hint: resp.hint });
      queryClient.invalidateQueries({ queryKey: queryKeys.adminApiKey() });
      setPlainKey(resp.key ?? '');
      setShowKeyModal(true);
      setConfirmRegen(false);
      toast(
        'success',
        hasKey
          ? t('settings.security_admin_key_regenerated')
          : t('settings.security_admin_key_generated'),
      );
    },
    onError: (err: Error) => toast('error', err.message),
  });

  const deleteMutation = useMutation({
    mutationFn: () => adminApiKeyApi.remove(),
    onSuccess: () => {
      queryClient.setQueryData(queryKeys.adminApiKey(), null);
      queryClient.invalidateQueries({ queryKey: queryKeys.adminApiKey() });
      setConfirmDelete(false);
      toast('success', t('settings.security_admin_key_deleted'));
    },
    onError: (err: Error) => toast('error', err.message),
  });
  const showKeyModalState = useOverlayState({
    isOpen: showKeyModal,
    onOpenChange: (nextOpen) => {
      if (!nextOpen) {
        setShowKeyModal(false);
        setPlainKey('');
      }
    },
  });

  return (
    <>
      <SettingsSection
        description={t('settings.security_admin_key_desc')}
        title={t('settings.security_admin_key')}
      >
        <div className="mb-4">
          <Alert status="warning">
            <Alert.Content>
              <Alert.Description>{t('settings.security_admin_key_warning')}</Alert.Description>
            </Alert.Content>
          </Alert>
        </div>

        {isLoading ? (
          <div className="flex items-center py-4 text-text-tertiary text-sm">
            <Loader2 className="w-4 h-4 animate-spin mr-2" />
            {t('common.loading')}
          </div>
        ) : (
          <div className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
            <div className="min-w-0 flex-1">
              <div className="text-[12px] text-text-tertiary mb-1.5">
                {t('settings.security_admin_key_current')}
              </div>
              {hasKey ? (
                <code className="inline-block px-2.5 py-1.5 rounded-md bg-surface border border-glass-border text-[13px] font-mono text-text break-all">
                  {data!.hint}
                </code>
              ) : (
                <span className="text-[13px] text-text-tertiary">
                  {t('settings.security_admin_key_none')}
                </span>
              )}
            </div>

            <div className="flex items-center gap-2 shrink-0">
              {hasKey ? (
                <>
                  <Button
                    size="sm"
                    variant="secondary"
                    onPress={() => setConfirmRegen(true)}
                    isDisabled={generateMutation.isPending}
                    aria-busy={generateMutation.isPending}
                  >
                    <RotateCcw className="w-3.5 h-3.5" />
                    {t('settings.security_admin_key_regenerate')}
                  </Button>
                  <Button
                    size="sm"
                    variant="danger"
                    onPress={() => setConfirmDelete(true)}
                    isDisabled={deleteMutation.isPending}
                    aria-busy={deleteMutation.isPending}
                  >
                    <Trash2 className="w-3.5 h-3.5" />
                    {t('settings.security_admin_key_delete')}
                  </Button>
                </>
              ) : (
                <Button
                  size="sm"
                  onPress={() => generateMutation.mutate()}
                  isDisabled={generateMutation.isPending}
                  aria-busy={generateMutation.isPending}
                >
                  <KeyRound className="w-3.5 h-3.5" />
                  {t('settings.security_admin_key_generate')}
                </Button>
              )}
            </div>
          </div>
        )}
      </SettingsSection>

      <Modal state={showKeyModalState}>
        <DialogTriggerShim />
        <Modal.Backdrop>
          <Modal.Container placement="center" scroll="inside" size="md">
            <Modal.Dialog
              className="ag-elevation-modal"
              style={{ maxWidth: '520px', width: 'min(100%, calc(100vw - 2rem))' }}
            >
              <Modal.Header>
                <Modal.Heading>{t('settings.security_admin_key_show_title')}</Modal.Heading>
                <Modal.CloseTrigger />
              </Modal.Header>
              <Modal.Body>
                <div className="space-y-3">
                  <Alert status="warning">
                    <Alert.Content>
                      <Alert.Description>{t('settings.security_admin_key_show_hint')}</Alert.Description>
                    </Alert.Content>
                  </Alert>
                  <div className="flex items-center gap-2">
                    <code className="flex-1 min-w-0 px-3 py-2 rounded-md bg-surface border border-glass-border text-[13px] font-mono text-text break-all">
                      {plainKey}
                    </code>
                    <Button
                      size="sm"
                      variant="secondary"
                      onPress={() => copy(plainKey)}
                    >
                      <Copy className="w-3.5 h-3.5" />
                      {t('settings.security_admin_key_copy')}
                    </Button>
                  </div>
                </div>
              </Modal.Body>
              <Modal.Footer>
                <Button
                  onPress={() => {
                    setShowKeyModal(false);
                    setPlainKey('');
                  }}
                >
                  {t('common.confirm')}
                </Button>
              </Modal.Footer>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal>

      <AlertDialog isOpen={confirmRegen} onOpenChange={setConfirmRegen}>
        <DialogTriggerShim />
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="sm">
            <AlertDialog.Dialog className="ag-elevation-modal">
              <AlertDialog.Header>
                <AlertDialog.Icon status="danger" />
                <AlertDialog.Heading>{t('settings.security_admin_key_regenerate_confirm_title')}</AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>{t('settings.security_admin_key_regenerate_confirm_msg')}</AlertDialog.Body>
              <AlertDialog.Footer>
                <Button variant="secondary" onPress={() => setConfirmRegen(false)}>
                  {t('common.cancel')}
                </Button>
                <Button
                  aria-busy={generateMutation.isPending}
                  isDisabled={generateMutation.isPending}
                  variant="danger"
                  onPress={() => generateMutation.mutate()}
                >
                  {generateMutation.isPending ? <Spinner size="sm" /> : null}
                  {t('common.confirm')}
                </Button>
              </AlertDialog.Footer>
            </AlertDialog.Dialog>
          </AlertDialog.Container>
        </AlertDialog.Backdrop>
      </AlertDialog>

      <AlertDialog isOpen={confirmDelete} onOpenChange={setConfirmDelete}>
        <DialogTriggerShim />
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="sm">
            <AlertDialog.Dialog className="ag-elevation-modal">
              <AlertDialog.Header>
                <AlertDialog.Icon status="danger" />
                <AlertDialog.Heading>{t('settings.security_admin_key_delete_confirm_title')}</AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>{t('settings.security_admin_key_delete_confirm_msg')}</AlertDialog.Body>
              <AlertDialog.Footer>
                <Button variant="secondary" onPress={() => setConfirmDelete(false)}>
                  {t('common.cancel')}
                </Button>
                <Button
                  aria-busy={deleteMutation.isPending}
                  isDisabled={deleteMutation.isPending}
                  variant="danger"
                  onPress={() => deleteMutation.mutate()}
                >
                  {deleteMutation.isPending ? <Spinner size="sm" /> : null}
                  {t('common.confirm')}
                </Button>
              </AlertDialog.Footer>
            </AlertDialog.Dialog>
          </AlertDialog.Container>
        </AlertDialog.Backdrop>
      </AlertDialog>
    </>
  );
}

// ==================== Email Template Editor ====================

function EmailTemplateEditor({
  subject,
  body,
  onSubjectChange,
  onBodyChange,
  siteName,
  variables,
  isPreviewOpen,
  onPreviewOpenChange,
}: {
  subject: string;
  body: string;
  onSubjectChange: (v: string) => void;
  onBodyChange: (v: string) => void;
  siteName: string;
  variables: { name: string; sample: string }[];
  isPreviewOpen: boolean;
  onPreviewOpenChange: (isOpen: boolean) => void;
}) {
  const { t } = useTranslation();

  // 模板变量替换预览
  function replaceVars(text: string) {
    let result = text;
    for (const v of variables) {
      result = result.replace(new RegExp(`\\{\\{${v.name}\\}\\}`, 'g'), v.sample);
    }
    return result;
  }

  const previewHtml = replaceVars(body);
  const previewModalState = useOverlayState({
    isOpen: isPreviewOpen,
    onOpenChange: onPreviewOpenChange,
  });

  return (
    <>
      <div className="space-y-4">
        <div className="text-[11px] text-text-tertiary space-x-3">
          <span>{t('settings.template_vars')}:</span>
          {variables.map((v) => (
            <code key={v.name} className="px-1.5 py-0.5 rounded bg-surface border border-glass-border text-primary">{`{{${v.name}}}`}</code>
          ))}
        </div>
        <Field label={t('settings.template_subject')}>
          <Input value={subject} onChange={(e) => onSubjectChange(e.target.value)} />
        </Field>
        <Field label={t('settings.template_body')} hint={t('settings.template_body_hint')}>
          <TextArea
            aria-label={t('settings.template_body')}
            value={body}
            onChange={(e) => onBodyChange(e.target.value)}
            className="h-80 w-full font-mono text-xs leading-5"
          />
        </Field>
      </div>
      {isPreviewOpen ? (
        <Modal state={previewModalState}>
          <DialogTriggerShim />
          <Modal.Backdrop>
            <Modal.Container placement="center" scroll="inside" size="lg">
              <Modal.Dialog
                className="ag-elevation-modal"
                style={{ maxWidth: '820px', width: 'min(100%, calc(100vw - 2rem))' }}
              >
                <Modal.Header>
                  <Modal.Heading>{t('settings.template_preview')}</Modal.Heading>
                  <Modal.CloseTrigger />
                </Modal.Header>
                <Modal.Body>
                  <div className="overflow-hidden rounded-xl border border-glass-border bg-overlay shadow-sm">
                    <div className="space-y-0.5 border-b border-glass-border bg-bg-hover/50 px-4 py-2.5 text-[11px]">
                      <div className="flex gap-2">
                        <span className="w-8 shrink-0 text-text-tertiary">From</span>
                        <span className="text-text-secondary">{siteName}</span>
                      </div>
                      <div className="flex gap-2">
                        <span className="w-8 shrink-0 text-text-tertiary">To</span>
                        <span className="text-text-secondary">user@example.com</span>
                      </div>
                      <div className="flex gap-2">
                        <span className="w-8 shrink-0 text-text-tertiary">Sub</span>
                        <span className="font-medium text-text">{replaceVars(subject)}</span>
                      </div>
                    </div>
                    <div className="max-h-[60vh] overflow-y-auto bg-[#f8f9fa] p-5">
                      <div dangerouslySetInnerHTML={{ __html: previewHtml }} />
                    </div>
                  </div>
                </Modal.Body>
                <Modal.Footer>
                  <Button onPress={() => onPreviewOpenChange(false)}>{t('common.close')}</Button>
                </Modal.Footer>
              </Modal.Dialog>
            </Modal.Container>
          </Modal.Backdrop>
        </Modal>
      ) : null}
    </>
  );
}

// ==================== Storage Panel ====================

function StoragePanel({
  set,
  boolVal,
  val,
  footer,
}: {
  set: (key: string, value: string) => void;
  boolVal: (key: string) => boolean;
  val: (key: string) => string;
  footer?: React.ReactNode;
}) {
  const { t } = useTranslation();

  return (
      <Card>
        <Card.Header>
          <Card.Title>{t('settings.storage_config')}</Card.Title>
        </Card.Header>
        <Card.Content>
          <Form className="grid grid-cols-1 md:grid-cols-2 gap-6" onSubmit={(e) => e.preventDefault()}>
          <Field label={t('settings.s3_endpoint')} hint={t('settings.s3_endpoint_hint')}>
            <Input
              value={val('s3_endpoint')}
              onChange={(e) => set('s3_endpoint', e.target.value)}
              placeholder="http://minio:9000"
            />
          </Field>
          <Field label={t('settings.s3_bucket')} hint={t('settings.s3_bucket_hint')}>
            <Input
              value={val('s3_bucket')}
              onChange={(e) => set('s3_bucket', e.target.value)}
              placeholder="airgate"
            />
          </Field>
          <Field label={t('settings.s3_access_key')}>
            <Input
              value={val('s3_access_key')}
              onChange={(e) => set('s3_access_key', e.target.value)}
              autoComplete="off"
            />
          </Field>
          <Field label={t('settings.s3_secret_key')}>
            <Input
              name="s3_secret_key"
              type="password"
              value={val('s3_secret_key')}
              onChange={(e) => set('s3_secret_key', e.target.value)}
              autoComplete="off"
            />
          </Field>
          <Field label={t('settings.s3_region')} hint={t('settings.s3_region_hint')}>
            <Input
              value={val('s3_region')}
              onChange={(e) => set('s3_region', e.target.value)}
              placeholder="us-east-1"
            />
          </Field>
          <Field label={t('settings.s3_presign_ttl_minutes')} hint={t('settings.s3_presign_ttl_minutes_hint')}>
            <Input
              type="number"
              value={val('s3_presign_ttl_minutes')}
              onChange={(e) => set('s3_presign_ttl_minutes', e.target.value)}
              placeholder="360"
            />
          </Field>
          <Field className="col-span-1 md:col-span-2" label={t('settings.s3_public_base_url')} hint={t('settings.s3_public_base_url_hint')}>
            <Input
              value={val('s3_public_base_url')}
              onChange={(e) => set('s3_public_base_url', e.target.value)}
              placeholder="https://cdn.example.com/airgate"
            />
          </Field>
          <Field label={t('settings.s3_path_prefix')} hint={t('settings.s3_path_prefix_hint')}>
            <Input
              value={val('s3_path_prefix')}
              onChange={(e) => set('s3_path_prefix', e.target.value)}
              placeholder="airgate"
            />
          </Field>
          <Field label={t('settings.local_storage_dir')} hint={t('settings.local_storage_dir_hint')}>
            <Input
              value={val('local_storage_dir')}
              onChange={(e) => set('local_storage_dir', e.target.value)}
              placeholder="data/assets"
            />
          </Field>
          <NativeSwitch
            className="col-span-1 md:col-span-2"
            isSelected={boolVal('s3_use_ssl')}
            label={(
              <>
                <span className="text-sm font-medium text-text">{t('settings.s3_use_ssl')}</span>
                <span className="block text-xs text-text-tertiary">{t('settings.s3_use_ssl_desc')}</span>
              </>
            )}
            onChange={(v) => set('s3_use_ssl', String(v))}
          />
          <div className="col-span-1 md:col-span-2 pt-2 border-t border-border">
            <div className="text-sm font-medium text-text">{t('settings.asset_retention_section')}</div>
            <div className="mt-1 text-xs text-text-tertiary">{t('settings.asset_retention_section_hint')}</div>
          </div>
          <Field label={t('settings.asset_retention_generated_days')} hint={t('settings.asset_retention_generated_days_hint')}>
            <Input
              type="number"
              value={val('asset_retention_generated_days')}
              onChange={(e) => set('asset_retention_generated_days', e.target.value)}
              placeholder="7"
            />
          </Field>
        </Form>
        {footer}
      </Card.Content>
    </Card>
  );
}

// ==================== OpenClaw Panel ====================

function OpenClawPanel({
  values,
  set,
  boolVal,
  val,
  footer,
}: {
  values: Record<string, string>;
  set: (key: string, value: string) => void;
  boolVal: (key: string) => boolean;
  val: (key: string) => string;
  footer?: React.ReactNode;
}) {
  const { t } = useTranslation();
  const copy = useClipboard();

  // 未设置时按钮态显示"启用"，即默认启用。
  const enabled = (values['openclaw.enabled'] ?? 'true') === 'true';

  // 管理员可能没填 site.api_base_url，这里只做展示预览，真正的 URL 推导在后端。
  // 都为空时回退到当前页面 origin（与 DocsPage 的处理一致），避免出现尴尬的 <站点地址> 占位符。
  const fallbackOrigin = typeof window !== 'undefined' ? window.location.origin : '';
  const usingFallbackOrigin = !val('openclaw.base_url') && !val('api_base_url');
  const previewBase = (val('openclaw.base_url') || val('api_base_url') || fallbackOrigin || '').replace(/\/$/, '');

  // 两个平台对应两份命令：Unix 用 bash + curl，Windows 用 PowerShell iwr|iex。
  // 后端 HandleInfo 同时返回 install_command_bash / install_command_powershell 两个字段，
  // 这里也分开展示，通过 tab 切换。
  const baseForCmd = previewBase || '<站点地址>';
  const installCommandBash = `curl -fsSL ${baseForCmd}/openclaw/install.sh -o openclaw-install.sh && bash openclaw-install.sh`;
  const installCommandPowerShell = `iwr -useb ${baseForCmd}/openclaw/install.ps1 | iex`;

  // 模型预设 JSON 的客户端校验：不阻塞保存，只给提示，让管理员自己决定。
  const modelsRaw = values['openclaw.models_preset'] ?? '';
  let modelsError = '';
  if (modelsRaw.trim() !== '') {
    try {
      const parsed = JSON.parse(modelsRaw);
      if (!Array.isArray(parsed)) {
        modelsError = t('settings.openclaw_models_not_array');
      }
    } catch (e) {
      modelsError = (e as Error).message;
    }
  }

  return (
    <Card>
      <Card.Header>
        <Card.Title>{t('settings.tab_openclaw')}</Card.Title>
      </Card.Header>
      <Card.Content>
        <div className="ag-settings-section-stack">
          <SettingsSection
            description={t('settings.openclaw_quickstart_desc')}
            title={t('settings.openclaw_quickstart')}
          >
            <div className="space-y-4">
              <div className="space-y-2">
                <div className="text-sm font-medium text-text">
                  {t('settings.openclaw_install_tab_unix')}
                </div>
                <div className="flex items-center gap-2">
                  <code className="flex-1 min-w-0 px-3 py-2 rounded-md bg-surface border border-glass-border text-[12px] font-mono text-text break-all">
                    {installCommandBash}
                  </code>
                  <Button
                    size="sm"
                    variant="secondary"
                    onPress={() => copy(installCommandBash)}
                    isDisabled={!previewBase}
                  >
                    <Copy className="w-3.5 h-3.5" />
                    {t('settings.openclaw_copy_command')}
                  </Button>
                </div>
              </div>

              <div className="space-y-2">
                <div className="text-sm font-medium text-text">
                  {t('settings.openclaw_install_tab_windows')}
                </div>
                <div className="flex items-center gap-2">
                  <code className="flex-1 min-w-0 px-3 py-2 rounded-md bg-surface border border-glass-border text-[12px] font-mono text-text break-all">
                    {installCommandPowerShell}
                  </code>
                  <Button
                    size="sm"
                    variant="secondary"
                    onPress={() => copy(installCommandPowerShell)}
                    isDisabled={!previewBase}
                  >
                    <Copy className="w-3.5 h-3.5" />
                    {t('settings.openclaw_copy_command')}
                  </Button>
                </div>
              </div>
            </div>
            {usingFallbackOrigin && (
              <p className="text-[11px] text-text-tertiary mt-2">
                {t('settings.openclaw_base_url_missing_hint')}
              </p>
            )}
          </SettingsSection>

          <SettingsSection title={t('settings.openclaw_basic')}>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
              <NativeSwitch
                className="col-span-1 md:col-span-2"
                isSelected={enabled}
                label={(
                  <>
                    <span className="text-sm font-medium text-text">{t('settings.openclaw_enabled')}</span>
                    <span className="block text-xs text-text-tertiary">{t('settings.openclaw_enabled_desc')}</span>
                  </>
                )}
                onChange={(v) => set('openclaw.enabled', String(v))}
              />
              <Field label={t('settings.openclaw_provider_name')} hint={t('settings.openclaw_provider_name_hint')}>
                <Input
                  value={val('openclaw.provider_name')}
                  onChange={(e) => set('openclaw.provider_name', e.target.value)}
                  placeholder={DEFAULT_OPENCLAW_PROVIDER_NAME}
                />
              </Field>
              <Field label={t('settings.openclaw_base_url')} hint={t('settings.openclaw_base_url_hint')}>
                <Input
                  value={val('openclaw.base_url')}
                  onChange={(e) => set('openclaw.base_url', e.target.value)}
                  placeholder="https://api.example.com"
                />
              </Field>
            </div>
          </SettingsSection>

          <SettingsSection title={t('settings.openclaw_memory_search')}>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
              <NativeSwitch
                className="col-span-1 md:col-span-2"
                isSelected={boolVal('openclaw.memory_search_enabled')}
                label={(
                  <>
                    <span className="text-sm font-medium text-text">{t('settings.openclaw_memory_search_enabled')}</span>
                    <span className="block text-xs text-text-tertiary">{t('settings.openclaw_memory_search_enabled_desc')}</span>
                  </>
                )}
                onChange={(v) => set('openclaw.memory_search_enabled', String(v))}
              />
              <Field label={t('settings.openclaw_memory_search_model')} hint={t('settings.openclaw_memory_search_model_hint')}>
                <Input
                  value={val('openclaw.memory_search_model')}
                  onChange={(e) => set('openclaw.memory_search_model', e.target.value)}
                  placeholder={DEFAULT_OPENCLAW_MEMORY_MODEL}
                />
              </Field>
            </div>
          </SettingsSection>

          <SettingsSection
            action={(
              <Button
                size="sm"
                variant="ghost"
                onPress={() => set('openclaw.models_preset', DEFAULT_OPENCLAW_MODELS_PRESET)}
              >
                <RotateCcw className="w-3.5 h-3.5" />
                {t('settings.template_reset')}
              </Button>
            )}
            description={t('settings.openclaw_models_preset_desc')}
            title={t('settings.openclaw_models_preset')}
          >
            <TextArea
              aria-label={t('settings.openclaw_models_preset')}
              value={modelsRaw || DEFAULT_OPENCLAW_MODELS_PRESET}
              onChange={(e) => set('openclaw.models_preset', e.target.value)}
              className="h-80 w-full font-mono text-xs leading-5"
              placeholder={DEFAULT_OPENCLAW_MODELS_PRESET}
            />
            {modelsError && (
              <p className="text-[11px] text-danger mt-1.5">{modelsError}</p>
            )}
          </SettingsSection>
        </div>
        {footer}
      </Card.Content>
    </Card>
  );
}

// ==================== Logo Upload ====================

function LogoUpload({ value, onChange }: { value: string; onChange: (url: string) => void }) {
  const { t } = useTranslation();
  const { toast } = useToast();
  const fileInputRef = useRef<HTMLInputElement>(null);

  const handleFile = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    if (file.size > 512 * 1024) {
      toast('error', t('settings.logo_too_large'));
      return;
    }
    const reader = new FileReader();
    reader.onload = () => {
      onChange(reader.result as string);
    };
    reader.readAsDataURL(file);
    e.target.value = '';
  };

  return (
    <div className="flex items-center gap-3">
      <div className="relative group">
        <img src={value || defaultLogoUrl} alt="Logo" className="w-14 h-14 rounded-sm object-cover" />
        {value && (
          <Button
            aria-label={t('settings.restore_default_logo')}
            className="absolute -top-1.5 -right-1.5 opacity-0 group-hover:opacity-100 transition-opacity"
            isIconOnly
            size="sm"
            variant="danger"
            onPress={() => onChange('')}
          >
            <X className="w-3 h-3" />
          </Button>
        )}
      </div>
      <div className="flex flex-col gap-1.5">
        <input
          ref={fileInputRef}
          type="file"
          accept="image/png,image/jpeg,image/svg+xml,image/x-icon,image/webp"
          onChange={handleFile}
          className="hidden"
        />
        <Button
          size="sm"
          variant="secondary"
          onPress={() => fileInputRef.current?.click()}
        >
          <Upload className="w-3.5 h-3.5" />
          {value ? t('settings.change_logo') : t('settings.upload_logo')}
        </Button>
        {value && (
          <Button
            size="sm"
            variant="ghost"
            onPress={() => onChange('')}
          >
            <RotateCcw className="w-3.5 h-3.5" />
            {t('settings.restore_default_logo')}
          </Button>
        )}
      </div>
    </div>
  );
}

// ==================== Field wrapper ====================

// ==================== 模型目录覆盖层编辑器 ====================
//
// 纯覆盖层语义:只列"你要新增/改价/上下架"的模型,不重复内置全表(内置价是不变的地板)。
// 表单字段全部用字符串(便于数字输入控制),序列化成各插件 models.catalog.<platform> 期望的 JSON:
//   [{ id, upstream_id?, name?, context_window?, max_output_tokens?, enabled?,
//      pricing?{input,cached_input,cache_write_5m,cache_write_1h,output,
//               priority_*,flex_*(仅 openai)}, long_context?{...}(仅 openai) }]
// 第①层基础价(本编辑器)× 第②层分组倍率(分组管理,独立不动)= 用户实际扣费。
//
// 序列化以条目原始 JSON(raw)为底做合并:表单没有呈现的字段原样保留,
// 防止"打开设置页保存一次"就把三档价/长上下文等高级字段静默抹掉。

// 每行分配一个稳定 uid，作 React key 与折叠态的身份锚点，
// 避免用数组下标当 key 时"删中间一行→下面行的展开态错位"。
// 序列化只认已知业务字段 + raw，uid 不落库。
let catalogUidSeq = 1;
function nextCatalogUid(): number { return catalogUidSeq++; }

interface CatalogRow {
  uid: number;
  id: string;
  upstreamID: string;
  name: string;
  contextWindow: string;
  maxOutput: string;
  enabled: boolean;
  input: string;
  cachedInput: string;
  cacheWrite5m: string;
  cacheWrite1h: string;
  output: string;
  // OpenAI 专属:Priority / Flex 档与长上下文阶梯(其余平台不渲染、不改写)
  prioInput: string;
  prioCached: string;
  prioOutput: string;
  flexInput: string;
  flexCached: string;
  flexOutput: string;
  lcThreshold: string;
  lcInputMult: string;
  lcCachedMult: string;
  lcOutputMult: string;
  // 展示口径(不影响计费):currency="CNY" 表示基准价是官方人民币牌价按 1:1 记账;
  // official* 为官方美元直付参考价,供前台划线对比与折扣换算
  currency: string;
  officialInput: string;
  officialCached: string;
  officialOutput: string;
  // Seedance 专属:档位(新增模型声明桶价基底)与桶价表(桶键 → 价字符串)
  tier: string;
  buckets: Record<string, string>;
  // 原始 JSON 条目,序列化时作合并底座(未知字段无损保留)
  raw: Record<string, unknown>;
}

function emptyCatalogRow(): CatalogRow {
  return {
    uid: nextCatalogUid(),
    id: '', upstreamID: '', name: '', contextWindow: '', maxOutput: '', enabled: true,
    input: '', cachedInput: '', cacheWrite5m: '', cacheWrite1h: '', output: '',
    prioInput: '', prioCached: '', prioOutput: '', flexInput: '', flexCached: '', flexOutput: '',
    lcThreshold: '', lcInputMult: '', lcCachedMult: '', lcOutputMult: '',
    currency: '', officialInput: '', officialCached: '', officialOutput: '',
    tier: '', buckets: {},
    raw: {},
  };
}

function parseCatalogRows(raw: string, isSeedance = false): CatalogRow[] {
  const trimmed = (raw ?? '').trim();
  if (!trimmed) return [];
  let arr: unknown;
  try { arr = JSON.parse(trimmed); } catch { return []; }
  if (!Array.isArray(arr)) return [];
  const numStr = (v: unknown) => (typeof v === 'number' && v !== 0 ? String(v) : (typeof v === 'string' ? v : ''));
  const priceStr = (v: unknown) => (typeof v === 'number' ? String(v) : (typeof v === 'string' ? v : ''));
  const asObj = (v: unknown): Record<string, unknown> =>
    (v && typeof v === 'object' && !Array.isArray(v) ? v as Record<string, unknown> : {});
  return arr.map((item) => {
    const e = asObj(item);
    const p = asObj(e.pricing);
    const lc = asObj(e.long_context);
    const official = asObj(e.official_pricing);
    // Seedance:pricing 是桶价 map(桶键 → 价),不是 token 的 input/output。
    const buckets: Record<string, string> = {};
    if (isSeedance) {
      for (const [k, v] of Object.entries(p)) buckets[k] = priceStr(v);
    }
    return {
      uid: nextCatalogUid(),
      id: typeof e.id === 'string' ? e.id : '',
      upstreamID: typeof e.upstream_id === 'string' ? e.upstream_id : (typeof e.kiro_id === 'string' ? e.kiro_id : ''),
      name: typeof e.name === 'string' ? e.name : '',
      contextWindow: numStr(e.context_window),
      maxOutput: numStr(e.max_output_tokens),
      enabled: e.enabled !== false,
      input: priceStr(p.input),
      cachedInput: priceStr(p.cached_input),
      cacheWrite5m: priceStr(p.cache_write_5m),
      cacheWrite1h: priceStr(p.cache_write_1h),
      output: priceStr(p.output),
      prioInput: priceStr(p.priority_input),
      prioCached: priceStr(p.priority_cached_input),
      prioOutput: priceStr(p.priority_output),
      flexInput: priceStr(p.flex_input),
      flexCached: priceStr(p.flex_cached_input),
      flexOutput: priceStr(p.flex_output),
      lcThreshold: numStr(lc.threshold),
      lcInputMult: priceStr(lc.input_multiplier),
      lcCachedMult: priceStr(lc.cached_multiplier),
      lcOutputMult: priceStr(lc.output_multiplier),
      currency: typeof e.currency === 'string' ? e.currency : '',
      officialInput: priceStr(official.input),
      officialCached: priceStr(official.cached_input),
      officialOutput: priceStr(official.output),
      tier: typeof e.tier === 'string' ? e.tier : '',
      buckets,
      raw: e,
    };
  });
}

// serializeSeedanceRow 序列化视频模型条目:{ id, name?, tier?, enabled?, pricing:{桶→价} }。
// 只写填了正数的桶(空=沿用内置地板价);不写 token 价 / context / long_context。
function serializeSeedanceRow(
  r: CatalogRow,
  setNum: (obj: Record<string, unknown>, key: string, value: string) => void,
): Record<string, unknown> {
  const entry: Record<string, unknown> = { ...r.raw, id: r.id.trim() };
  if (r.name.trim()) entry.name = r.name.trim(); else delete entry.name;
  if (r.tier.trim()) entry.tier = r.tier.trim(); else delete entry.tier;
  // 清掉 token 平台残留字段,防形状污染
  delete entry.upstream_id; delete entry.kiro_id;
  delete entry.context_window; delete entry.max_output_tokens; delete entry.long_context;
  if (!r.enabled) entry.enabled = false; else delete entry.enabled;

  const rawPricing = (entry.pricing && typeof entry.pricing === 'object' && !Array.isArray(entry.pricing))
    ? { ...(entry.pricing as Record<string, unknown>) } : {};
  for (const bucket of SEEDANCE_BUCKETS) setNum(rawPricing, bucket, r.buckets[bucket] ?? '');
  if (Object.keys(rawPricing).length > 0) entry.pricing = rawPricing; else delete entry.pricing;
  return entry;
}

function serializeCatalogRows(rows: CatalogRow[], isOpenAI: boolean, isSeedance = false): string {
  const setNum = (obj: Record<string, unknown>, key: string, value: string) => {
    const n = Number(value);
    if (value.trim() && Number.isFinite(n) && n > 0) obj[key] = n; else delete obj[key];
  };
  const out = rows
    .filter((r) => r.id.trim() !== '')
    .map((r) => {
      if (isSeedance) return serializeSeedanceRow(r, setNum);
      const entry: Record<string, unknown> = { ...r.raw, id: r.id.trim() };
      if (r.name.trim()) entry.name = r.name.trim(); else delete entry.name;
      // upstream_id 与 kiro_id 是同义旧写法,统一写 upstream_id、清掉旧键防漂移
      delete entry.kiro_id;
      if (r.upstreamID.trim()) entry.upstream_id = r.upstreamID.trim(); else delete entry.upstream_id;
      setNum(entry, 'context_window', r.contextWindow);
      setNum(entry, 'max_output_tokens', r.maxOutput);
      if (!r.enabled) entry.enabled = false; else delete entry.enabled;

      const rawPricing = (entry.pricing && typeof entry.pricing === 'object' && !Array.isArray(entry.pricing))
        ? { ...(entry.pricing as Record<string, unknown>) } : {};
      setNum(rawPricing, 'input', r.input);
      setNum(rawPricing, 'cached_input', r.cachedInput);
      setNum(rawPricing, 'output', r.output);
      if (isOpenAI) {
        setNum(rawPricing, 'priority_input', r.prioInput);
        setNum(rawPricing, 'priority_cached_input', r.prioCached);
        setNum(rawPricing, 'priority_output', r.prioOutput);
        setNum(rawPricing, 'flex_input', r.flexInput);
        setNum(rawPricing, 'flex_cached_input', r.flexCached);
        setNum(rawPricing, 'flex_output', r.flexOutput);
      } else {
        setNum(rawPricing, 'cache_write_5m', r.cacheWrite5m);
        setNum(rawPricing, 'cache_write_1h', r.cacheWrite1h);
      }
      if (Object.keys(rawPricing).length > 0) entry.pricing = rawPricing; else delete entry.pricing;

      if (isOpenAI) {
        const rawLC = (entry.long_context && typeof entry.long_context === 'object' && !Array.isArray(entry.long_context))
          ? { ...(entry.long_context as Record<string, unknown>) } : {};
        setNum(rawLC, 'threshold', r.lcThreshold);
        setNum(rawLC, 'input_multiplier', r.lcInputMult);
        setNum(rawLC, 'cached_multiplier', r.lcCachedMult);
        setNum(rawLC, 'output_multiplier', r.lcOutputMult);
        if (Object.keys(rawLC).length > 0) entry.long_context = rawLC; else delete entry.long_context;
      }

      // 展示口径(不影响计费):currency 仅在非默认(CNY)时落库;官方美元参考价同 raw 合并
      if (r.currency.trim() && r.currency.trim().toUpperCase() !== 'USD') {
        entry.currency = r.currency.trim().toUpperCase();
      } else {
        delete entry.currency;
      }
      const rawOfficial = (entry.official_pricing && typeof entry.official_pricing === 'object' && !Array.isArray(entry.official_pricing))
        ? { ...(entry.official_pricing as Record<string, unknown>) } : {};
      setNum(rawOfficial, 'input', r.officialInput);
      setNum(rawOfficial, 'cached_input', r.officialCached);
      setNum(rawOfficial, 'output', r.officialOutput);
      if (Object.keys(rawOfficial).length > 0) entry.official_pricing = rawOfficial; else delete entry.official_pricing;
      return entry;
    });
  return JSON.stringify(out, null, 2);
}

function catalogPriceFields(r: CatalogRow, isOpenAI: boolean, isSeedance: boolean): string[] {
  if (isSeedance) return SEEDANCE_BUCKETS.map((b) => r.buckets[b] ?? '');
  const shared = [r.input, r.cachedInput, r.output, r.officialInput, r.officialCached, r.officialOutput];
  return isOpenAI
    ? [...shared, r.prioInput, r.prioCached, r.prioOutput, r.flexInput, r.flexCached, r.flexOutput,
      r.lcThreshold, r.lcInputMult, r.lcCachedMult, r.lcOutputMult]
    : [...shared, r.cacheWrite5m, r.cacheWrite1h];
}

function validateCatalogRows(rows: CatalogRow[], isOpenAI: boolean, isSeedance: boolean, t: (k: string, o?: Record<string, unknown>) => string): string[] {
  const errs: string[] = [];
  const seen = new Set<string>();
  // 允许 org/model 形式(如 zai-org/glm-5.2-fp8)——上游标准命名带斜杠,插件侧 normalizeID 原样匹配
  const idRe = /^[a-zA-Z0-9._/-]+$/;
  rows.forEach((r, i) => {
    const id = r.id.trim();
    if (!id) { errs.push(t('settings.models_err_id_empty', { n: i + 1 })); return; }
    if (!idRe.test(id)) errs.push(t('settings.models_err_id_invalid', { id }));
    if (r.upstreamID.trim() && !idRe.test(r.upstreamID.trim())) {
      errs.push(t('settings.models_err_upstream_invalid', { id: r.upstreamID.trim() }));
    }
    if (seen.has(id)) errs.push(t('settings.models_err_dup', { id }));
    seen.add(id);
    catalogPriceFields(r, isOpenAI, isSeedance).forEach((p) => {
      if (p.trim() !== '' && !(Number(p) > 0)) errs.push(t('settings.models_err_price', { id }));
    });
  });
  return errs;
}

function ModelCatalogPanel({ values, set, footer, onValidationChange }: {
  values: Record<string, string>;
  set: (key: string, value: string) => void;
  footer: React.ReactNode;
  onValidationChange: (key: ModelCatalogSettingKey, errors: string[]) => void;
}) {
  const { t } = useTranslation();
  // 各平台内置模型目录（插件上报,含 price.* 内置价提示）,给每个编辑器铺全量种子行。
  const { data: builtinCatalog } = useQuery({
    queryKey: queryKeys.builtinModels(),
    queryFn: modelsApi.builtin,
    staleTime: 60_000,
  });
  const builtinByPlatform = new Map<string, BuiltinModel[]>(
    (builtinCatalog ?? []).map((p) => [p.platform, p.models]),
  );
  return (
    <Card>
      <Card.Header>
        <Card.Title>{t('settings.tab_models')}</Card.Title>
      </Card.Header>
      <Card.Content>
        <div className="ag-settings-section-stack">
          {MODEL_CATALOG_PLATFORMS.map((platform) => (
            <ModelCatalogEditor
              key={platform.key}
              label={t(platform.labelKey)}
              settingKey={platform.key}
              set={set}
              value={values[platform.key] ?? ''}
              builtinModels={builtinByPlatform.get(platform.key.split('.').pop() ?? '') ?? []}
              onValidationChange={onValidationChange}
            />
          ))}
        </div>
        {footer}
      </Card.Content>
    </Card>
  );
}

// formatContextWindow 200000 → "200K"、1050000 → "1.05M"。
function formatContextWindow(n: number): string {
  if (!n || n <= 0) return '';
  if (n >= 1_000_000) {
    const m = n / 1_000_000;
    return `${Number.isInteger(m) ? m : m.toFixed(2)}M`;
  }
  return `${Math.round(n / 1000)}K`;
}

// builtinPriceSummary 从 metadata 的 price.* 提示键拼一行内置价摘要。
function builtinPriceSummary(meta: Record<string, string> | undefined, cachedLabel: string): string {
  if (!meta) return '';
  const input = meta['price.input'];
  const output = meta['price.output'];
  if (!input && !output) {
    // 视频模型:无 input/output,取一个代表桶价(优先 480p_no_ref)作提示。
    const videoPrefix = 'price.video_tokens.';
    const repKey = meta[`${videoPrefix}480p_no_ref`] ? `${videoPrefix}480p_no_ref`
      : Object.keys(meta).find((k) => k.startsWith(videoPrefix));
    return repKey ? `$${meta[repKey]} · ${repKey.slice(videoPrefix.length).toUpperCase()}` : '';
  }
  let s = `$${input ?? '—'} / $${output ?? '—'}`;
  if (meta['price.cached_input']) s += ` · ${cachedLabel} $${meta['price.cached_input']}`;
  return s;
}

function ModelCatalogEditor({ label, settingKey, set, value, builtinModels, onValidationChange }: {
  label: string;
  settingKey: ModelCatalogSettingKey;
  set: (key: string, value: string) => void;
  value: string;
  builtinModels: BuiltinModel[];
  onValidationChange: (key: ModelCatalogSettingKey, errors: string[]) => void;
}) {
  const { t } = useTranslation();
  const isOpenAI = settingKey === 'models.catalog.openai';
  const isSeedance = settingKey === 'models.catalog.seedance';
  // 本地 state 保证数字输入流畅(避免每键 parse→serialize 丢失尾字符);挂载时从 setting 初始化。
  // 页面在 settings 加载完成前显示全局 spinner,故挂载时 values 已就绪。
  const [rows, setRows] = useState<CatalogRow[]>(() => parseCatalogRows(value, isSeedance));
  // Provider 分区可折叠：默认只展开"有覆盖条目"的平台，空平台收起省地方。
  const [sectionOpen, setSectionOpen] = useState<boolean>(() => rows.length > 0);
  // 每行展开态按 uid 记（不用下标，删中间行不会错位）；默认仅展开尚未填 id 的新行。
  const [openUids, setOpenUids] = useState<Set<number>>(() => new Set(rows.filter((r) => !r.id.trim()).map((r) => r.uid)));
  const toggleRow = (uid: number) => setOpenUids((s) => {
    const next = new Set(s);
    if (next.has(uid)) next.delete(uid); else next.add(uid);
    return next;
  });

  function commit(next: CatalogRow[]) {
    setRows(next);
    set(settingKey, next.some((r) => r.id.trim() !== '') ? serializeCatalogRows(next, isOpenAI, isSeedance) : '');
  }
  const update = (i: number, patch: Partial<CatalogRow>) =>
    commit(rows.map((r, idx) => (idx === i ? { ...r, ...patch } : r)));
  const updateBucket = (i: number, bucket: string, value: string) =>
    commit(rows.map((r, idx) => (idx === i ? { ...r, buckets: { ...r.buckets, [bucket]: value } } : r)));
  const addRow = () => {
    const row = emptyCatalogRow();
    setSectionOpen(true);
    setOpenUids((s) => new Set(s).add(row.uid));
    commit([...rows, row]);
  };
  const removeRow = (i: number) => commit(rows.filter((_, idx) => idx !== i));

  // 内置模型铺底：过滤掉已有覆盖条目的,剩下的展示为只读行,点「覆盖」转成可编辑条目。
  const coveredIds = new Set(rows.map((r) => r.id.trim().toLowerCase()).filter(Boolean));
  const uncoveredBuiltin = builtinModels.filter((m) => !coveredIds.has(m.id.toLowerCase()));
  const overrideBuiltin = (m: BuiltinModel) => {
    const row = { ...emptyCatalogRow(), id: m.id, name: m.name };
    setSectionOpen(true);
    setOpenUids((s) => new Set(s).add(row.uid));
    commit([...rows, row]);
  };

  const errors = validateCatalogRows(rows, isOpenAI, isSeedance, t);
  const errorKey = errors.join('\n');
  useEffect(() => {
    onValidationChange(settingKey, errors);
    return () => onValidationChange(settingKey, []);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [settingKey, errorKey, onValidationChange]);

  // 有校验错误时强制展开，保证"保存被禁用"的原因（错误列表）不被折叠藏掉。
  const effectiveOpen = sectionOpen || errors.length > 0;

  return (
    <SettingsSection
      collapsible
      open={effectiveOpen}
      onToggleOpen={() => setSectionOpen((v) => !v)}
      badge={rows.length > 0 ? rows.length : undefined}
      action={(
        <Button size="sm" variant="ghost" onPress={addRow}>
          <Plus className="w-3.5 h-3.5" />
          {t('settings.models_add')}
        </Button>
      )}
      help={t('settings.models_catalog_desc')}
      title={`${t('settings.models_catalog')} · ${label}`}
    >
      {rows.length === 0 ? (
        <p className="text-[12px] leading-5 text-text-tertiary">{t('settings.models_empty')}</p>
      ) : (
        <div className="space-y-2">
          {rows.map((r, i) => {
            const rowOpen = openUids.has(r.uid);
            // Seedance:取该模型的内置桶价 metadata,用于灰色占位回显(空字段＝沿用内置,不钉死)。
            const builtinVideoMeta = isSeedance
              ? builtinModels.find((m) => m.id.toLowerCase() === r.id.trim().toLowerCase())?.metadata
              : undefined;
            const seedanceRepPrice = isSeedance
              ? (SEEDANCE_BUCKETS.map((b) => r.buckets[b]).find((v) => v && v.trim())
                || builtinVideoMeta?.['price.video_tokens.480p_no_ref'] || '—')
              : '';
            return (
            <div key={r.uid} className="rounded-lg border border-glass-border">
              <div className="flex items-center gap-2 px-3 py-2">
                <button
                  type="button"
                  onClick={() => toggleRow(r.uid)}
                  aria-expanded={rowOpen}
                  className="flex min-w-0 flex-1 items-center gap-2 text-left"
                >
                  <ChevronDown className={`h-4 w-4 shrink-0 text-text-tertiary transition-transform ${rowOpen ? '' : '-rotate-90'}`} />
                  <span className="truncate font-mono text-[13px] text-text">{r.id.trim() || '—'}</span>
                  {r.name.trim() ? <span className="truncate text-[12px] text-text-tertiary">{r.name.trim()}</span> : null}
                  {!r.enabled ? <span className="shrink-0 text-[10px] font-medium uppercase text-text-tertiary">off</span> : null}
                  <span className="ml-auto shrink-0 font-mono text-[12px] tabular-nums text-text-tertiary">{isSeedance
                    ? `${r.tier.trim() || t('settings.models_tier_inherit')} · $${seedanceRepPrice}`
                    : `${r.currency.trim().toUpperCase() === 'CNY' ? '¥' : '$'}${r.input.trim() || '—'} / ${r.currency.trim().toUpperCase() === 'CNY' ? '¥' : '$'}${r.output.trim() || '—'}`}</span>
                </button>
                <Button size="sm" variant="ghost" onPress={() => removeRow(i)} aria-label={t('common.delete')}>
                  <Trash2 className="w-3.5 h-3.5" />
                </Button>
              </div>
              {rowOpen ? (
              <div className="border-t border-glass-border p-3 space-y-2.5">
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
                <Field label={t('settings.models_field_id')}>
                  <Input value={r.id} onChange={(e) => update(i, { id: e.target.value })} placeholder={isSeedance ? 'dreamina-seedance-2-0-xxx' : 'claude-xxx / gpt-xxx'} />
                </Field>
                {!isSeedance && (
                  <Field label={t('settings.models_field_upstream')}>
                    <Input value={r.upstreamID} onChange={(e) => update(i, { upstreamID: e.target.value })} placeholder="optional upstream id" />
                  </Field>
                )}
                <Field label={t('settings.models_field_name')}>
                  <Input value={r.name} onChange={(e) => update(i, { name: e.target.value })} placeholder={isSeedance ? 'Seedance 2.0 XXX' : 'Claude XXX / GPT XXX'} />
                </Field>
                {isSeedance && (
                  <Field label={t('settings.models_field_tier')}>
                    <select
                      className="w-full rounded-md border border-glass-border bg-transparent px-2 py-2 text-[13px] text-text"
                      value={r.tier}
                      onChange={(e) => update(i, { tier: e.target.value })}
                    >
                      <option value="">{t('settings.models_tier_inherit')}</option>
                      {SEEDANCE_TIERS.map((tr) => <option key={tr} value={tr}>{tr}</option>)}
                    </select>
                  </Field>
                )}
                {!isSeedance && (
                  <Field label={t('settings.models_field_context')}>
                    <Input type="number" value={r.contextWindow} onChange={(e) => update(i, { contextWindow: e.target.value })} placeholder="1000000" />
                  </Field>
                )}
                {!isSeedance && (
                  <Field label={t('settings.models_field_maxout')}>
                    <Input type="number" value={r.maxOutput} onChange={(e) => update(i, { maxOutput: e.target.value })} placeholder="128000" />
                  </Field>
                )}
              </div>
              {isSeedance ? (
                <div>
                  <Label className="block text-[13px] font-medium text-text-secondary mb-1.5">
                    {t('settings.models_price_video_label')}
                  </Label>
                  <div className="space-y-2">
                    {SEEDANCE_RESOLUTIONS.map((res) => (
                      <div key={res} className="grid grid-cols-[3rem_1fr_1fr] items-end gap-3">
                        <span className="pb-2 font-mono text-[12px] uppercase text-text-secondary">{res}</span>
                        {SEEDANCE_REFS.map((ref) => {
                          const bucket = `${res}_${ref.suffix}`;
                          return (
                            <Field key={bucket} label={t(ref.labelKey)}>
                              <Input type="number" value={r.buckets[bucket] ?? ''} onChange={(e) => updateBucket(i, bucket, e.target.value)} placeholder={builtinVideoMeta?.[`price.video_tokens.${bucket}`] ?? '0'} />
                            </Field>
                          );
                        })}
                      </div>
                    ))}
                  </div>
                </div>
              ) : (
              <div>
                <Label className="block text-[13px] font-medium text-text-secondary mb-1.5">
                  {t('settings.models_price_label')}
                </Label>
                <div className="grid grid-cols-2 md:grid-cols-5 gap-3">
                  <Field label={t('settings.models_price_input')}>
                    <Input type="number" value={r.input} onChange={(e) => update(i, { input: e.target.value })} placeholder="0" />
                  </Field>
                  <Field label={t('settings.models_price_cached')}>
                    <Input type="number" value={r.cachedInput} onChange={(e) => update(i, { cachedInput: e.target.value })} placeholder={isOpenAI ? t('settings.models_ph_auto_cached') : '0'} />
                  </Field>
                  {!isOpenAI && (
                    <Field label={t('settings.models_price_w5m')}>
                      <Input type="number" value={r.cacheWrite5m} onChange={(e) => update(i, { cacheWrite5m: e.target.value })} placeholder={t('settings.models_ph_auto_w5m')} />
                    </Field>
                  )}
                  {!isOpenAI && (
                    <Field label={t('settings.models_price_w1h')}>
                      <Input type="number" value={r.cacheWrite1h} onChange={(e) => update(i, { cacheWrite1h: e.target.value })} placeholder={t('settings.models_ph_auto_w1h')} />
                    </Field>
                  )}
                  <Field label={t('settings.models_price_output')}>
                    <Input type="number" value={r.output} onChange={(e) => update(i, { output: e.target.value })} placeholder="0" />
                  </Field>
                </div>
              </div>
              )}
              {isOpenAI && (
                <div>
                  <Label className="block text-[13px] font-medium text-text-secondary mb-1.5">
                    {t('settings.models_price_prio_label')}
                  </Label>
                  <div className="grid grid-cols-3 gap-3">
                    <Field label={t('settings.models_price_input')}>
                      <Input type="number" value={r.prioInput} onChange={(e) => update(i, { prioInput: e.target.value })} placeholder={t('settings.models_ph_auto2')} />
                    </Field>
                    <Field label={t('settings.models_price_cached')}>
                      <Input type="number" value={r.prioCached} onChange={(e) => update(i, { prioCached: e.target.value })} placeholder={t('settings.models_ph_auto2')} />
                    </Field>
                    <Field label={t('settings.models_price_output')}>
                      <Input type="number" value={r.prioOutput} onChange={(e) => update(i, { prioOutput: e.target.value })} placeholder={t('settings.models_ph_auto2')} />
                    </Field>
                  </div>
                </div>
              )}
              {isOpenAI && (
                <div>
                  <Label className="block text-[13px] font-medium text-text-secondary mb-1.5">
                    {t('settings.models_price_flex_label')}
                  </Label>
                  <div className="grid grid-cols-3 gap-3">
                    <Field label={t('settings.models_price_input')}>
                      <Input type="number" value={r.flexInput} onChange={(e) => update(i, { flexInput: e.target.value })} placeholder={t('settings.models_ph_auto05')} />
                    </Field>
                    <Field label={t('settings.models_price_cached')}>
                      <Input type="number" value={r.flexCached} onChange={(e) => update(i, { flexCached: e.target.value })} placeholder={t('settings.models_ph_auto05')} />
                    </Field>
                    <Field label={t('settings.models_price_output')}>
                      <Input type="number" value={r.flexOutput} onChange={(e) => update(i, { flexOutput: e.target.value })} placeholder={t('settings.models_ph_auto05')} />
                    </Field>
                  </div>
                </div>
              )}
              {isOpenAI && (
                <div>
                  <Label className="block text-[13px] font-medium text-text-secondary mb-1.5">
                    {t('settings.models_price_lc_label')}
                  </Label>
                  <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
                    <Field label={t('settings.models_lc_threshold')}>
                      <Input type="number" value={r.lcThreshold} onChange={(e) => update(i, { lcThreshold: e.target.value })} placeholder="272000" />
                    </Field>
                    <Field label={t('settings.models_lc_input_mult')}>
                      <Input type="number" value={r.lcInputMult} onChange={(e) => update(i, { lcInputMult: e.target.value })} placeholder="2" />
                    </Field>
                    <Field label={t('settings.models_lc_cached_mult')}>
                      <Input type="number" value={r.lcCachedMult} onChange={(e) => update(i, { lcCachedMult: e.target.value })} placeholder="2" />
                    </Field>
                    <Field label={t('settings.models_lc_output_mult')}>
                      <Input type="number" value={r.lcOutputMult} onChange={(e) => update(i, { lcOutputMult: e.target.value })} placeholder="1.5" />
                    </Field>
                  </div>
                </div>
              )}
              {!isSeedance && (
                <div>
                  <Label className="block text-[13px] font-medium text-text-secondary mb-1.5">
                    {t('settings.models_display_label')}
                  </Label>
                  <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
                    <Field label={t('settings.models_display_currency')}>
                      <select
                        className="w-full rounded-md border border-glass-border bg-transparent px-2 py-2 text-[13px] text-text"
                        value={r.currency.trim().toUpperCase() === 'CNY' ? 'CNY' : 'USD'}
                        onChange={(e) => update(i, { currency: e.target.value })}
                      >
                        <option value="USD">{t('settings.models_currency_usd')}</option>
                        <option value="CNY">{t('settings.models_currency_cny')}</option>
                      </select>
                    </Field>
                    <Field label={t('settings.models_official_input')}>
                      <Input type="number" value={r.officialInput} onChange={(e) => update(i, { officialInput: e.target.value })} placeholder="1.4" />
                    </Field>
                    <Field label={t('settings.models_official_cached')}>
                      <Input type="number" value={r.officialCached} onChange={(e) => update(i, { officialCached: e.target.value })} placeholder="0.26" />
                    </Field>
                    <Field label={t('settings.models_official_output')}>
                      <Input type="number" value={r.officialOutput} onChange={(e) => update(i, { officialOutput: e.target.value })} placeholder="4.4" />
                    </Field>
                  </div>
                  <p className="mt-1 text-[11px] leading-4 text-text-tertiary">{t('settings.models_display_desc')}</p>
                </div>
              )}
              <div className="pt-1">
                <NativeSwitch
                  isSelected={r.enabled}
                  label={<span className="text-[13px] text-text-secondary">{t('settings.models_field_enabled')}</span>}
                  onChange={(v) => update(i, { enabled: v })}
                />
              </div>
              </div>
              ) : null}
            </div>
            );
          })}
        </div>
      )}
      {uncoveredBuiltin.length > 0 && (
        <div className="mt-4">
          <p className="mb-1.5 text-[12px] font-medium text-text-secondary">
            {t('settings.models_builtin_title')}
            <span className="ml-1.5 rounded-full border border-glass-border px-1.5 py-0.5 text-[10px] font-normal text-text-tertiary">{uncoveredBuiltin.length}</span>
          </p>
          <p className="mb-2 text-[11px] leading-4 text-text-tertiary">{t('settings.models_builtin_hint')}</p>
          <div className="divide-y divide-glass-border rounded-lg border border-glass-border">
            {uncoveredBuiltin.map((m) => {
              const price = builtinPriceSummary(m.metadata, t('settings.models_price_cached'));
              const ctx = formatContextWindow(m.context_window);
              return (
                <div key={m.id} className="flex items-center gap-2 px-3 py-1.5">
                  <span className="truncate font-mono text-[13px] text-text">{m.id}</span>
                  {m.name ? <span className="truncate text-[12px] text-text-tertiary">{m.name}</span> : null}
                  {ctx ? <span className="shrink-0 rounded border border-glass-border px-1 text-[10px] text-text-tertiary">{ctx}</span> : null}
                  <span className="ml-auto shrink-0 font-mono text-[12px] tabular-nums text-text-tertiary">{price}</span>
                  <Button size="sm" variant="ghost" onPress={() => overrideBuiltin(m)}>
                    {t('settings.models_builtin_override')}
                  </Button>
                </div>
              );
            })}
          </div>
        </div>
      )}
      {errors.length > 0 && (
        <ul className="mt-3 space-y-1">
          {errors.map((e, idx) => (
            <li key={idx} className="text-[11px] text-danger">{e}</li>
          ))}
        </ul>
      )}
    </SettingsSection>
  );
}

function SettingsSection({
  action,
  children,
  description,
  help,
  title,
  collapsible = false,
  open = true,
  onToggleOpen,
  badge,
}: {
  action?: React.ReactNode;
  children: React.ReactNode;
  description?: React.ReactNode;
  // help 详细说明:收进标题旁的 ⓘ,点开才展示,避免长注释常驻占地方。
  help?: React.ReactNode;
  title: React.ReactNode;
  collapsible?: boolean;
  open?: boolean;
  onToggleOpen?: () => void;
  badge?: React.ReactNode;
}) {
  const [showHelp, setShowHelp] = useState(false);
  const heading = (
    <div className="min-w-0">
      <h3 className="flex items-center gap-2 text-sm font-semibold text-text">
        {collapsible ? (
          <ChevronDown className={`h-4 w-4 shrink-0 text-text-tertiary transition-transform ${open ? '' : '-rotate-90'}`} />
        ) : null}
        <span className="truncate">{title}</span>
        {badge != null ? (
          <span className="shrink-0 rounded-full border border-glass-border px-2 py-0.5 text-[11px] font-normal text-text-tertiary">{badge}</span>
        ) : null}
      </h3>
      {description && open ? (
        <p className="mt-1 text-[12px] leading-5 text-text-tertiary">{description}</p>
      ) : null}
    </div>
  );
  return (
    <section className="ag-settings-section">
      <div className="ag-settings-section-heading">
        {collapsible ? (
          <button
            type="button"
            onClick={onToggleOpen}
            aria-expanded={open}
            className="flex min-w-0 flex-1 items-center text-left"
          >
            {heading}
          </button>
        ) : (
          heading
        )}
        <div className="flex shrink-0 items-center gap-1">
          {help ? (
            <button
              type="button"
              onClick={() => setShowHelp((v) => !v)}
              aria-expanded={showHelp}
              aria-label="help"
              className={`rounded p-1 transition-colors hover:bg-bg-hover ${showHelp ? 'text-text-secondary' : 'text-text-tertiary'}`}
            >
              <Info className="h-4 w-4" />
            </button>
          ) : null}
          {action ? <div>{action}</div> : null}
        </div>
      </div>
      {help && showHelp && open ? (
        <div className="mb-3 rounded-lg border border-glass-border bg-bg-subtle px-3 py-2 text-[12px] leading-5 text-text-tertiary">
          {help}
        </div>
      ) : null}
      {open ? <div className="ag-settings-section-body">{children}</div> : null}
    </section>
  );
}

function Field({
  className = '',
  label,
  hint,
  children,
}: {
  className?: string;
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <div className={`flex flex-col ${className}`}>
      <Label className="block text-[13px] font-medium text-text-secondary mb-1.5">
        {label}
      </Label>
      {children}
      {hint && <p className="text-[11px] text-text-tertiary mt-1">{hint}</p>}
    </div>
  );
}
