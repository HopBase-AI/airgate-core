import { Link, useNavigate } from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';
import { Button, Link as HeroLink } from '@heroui/react';
import { Activity, ArrowRight, Moon, ShieldCheck, Sun } from 'lucide-react';
import { useSiteSettings, defaultLogoUrl } from '../app/providers/SiteSettingsProvider';
import { useTheme } from '../app/providers/ThemeProvider';
import { useStatusPageEnabled } from '../shared/hooks/useStatusPageEnabled';
import { getToken } from '../shared/api/client';

const POLICY_UPDATED_AT = '2026年6月16日';

const sections = [
  {
    title: '1. 我们收集的信息',
    body: [
      '为提供账户、网关、计费、风控和技术支持服务，我们可能收集您主动提供或系统自动产生的必要信息，包括邮箱、用户名、账户状态、登录记录、订单记录、余额记录、订阅记录、API Key 元数据、用量统计和安全日志。',
      '当您调用 API 时，系统可能记录请求时间、模型名称、Token 或用量、费用、状态码、错误类型、IP、API Key 标识、路由结果、上游响应状态、耗时等请求元数据。',
    ],
  },
  {
    title: '2. API 请求内容',
    body: [
      '我们主要作为 API 网关和转发服务。通常情况下，您提交给模型的提示词、上下文、上传内容和模型返回内容仅用于完成当次接口转发和响应，不作为平台业务数据长期留存。',
      '我们不会为了训练模型而使用您的 API 请求内容，也不会主动人工审查您的完整对话内容。但在故障排查、安全风控、滥用调查、合规要求或您主动提交工单时，可能需要处理与问题相关的必要片段或日志。',
      '请不要在 API 请求中提交身份证件、银行卡、账户密码、私钥、助记词、医疗记录、商业秘密、未公开源代码或其他高度敏感信息。您主动提交此类信息产生的风险由您自行承担。',
    ],
  },
  {
    title: '3. 信息使用目的',
    body: [
      '我们使用必要信息来创建和维护账户、完成 API 转发、计算用量和费用、展示账单、处理充值或退款、提供技术支持、识别异常调用、限制滥用行为、保障平台安全以及履行合规义务。',
      '我们可能基于聚合或去标识化数据分析系统稳定性、模型成本、错误率、延迟和业务指标，以改进服务质量。',
    ],
  },
  {
    title: '4. 第三方处理',
    body: [
      '本服务可能依赖上游 AI 服务、云服务、支付机构、邮件服务、对象存储、分析、风控或客服系统。为完成您请求的功能，相关信息可能会被传输给相应第三方处理。',
      '第三方服务对数据的处理受其自身条款、隐私政策和数据处理规则约束。我们无法完全控制第三方的可用性、安全策略、数据留存规则、内容审核结果或合规判断。',
    ],
  },
  {
    title: '5. Cookie、本地存储与安全令牌',
    body: [
      '我们可能使用 Cookie、localStorage、sessionStorage 或类似技术保存登录状态、主题偏好、语言偏好、会话令牌和安全配置，以保证网站正常运行。',
      '请勿在不受信任的设备上保存登录状态。若您怀疑令牌或 API Key 泄露，应立即退出登录、重置密码或停用相关 Key。',
    ],
  },
  {
    title: '6. 数据保存与删除',
    body: [
      '我们会在实现服务目的所必需的期限内保存相关信息。账务、风控、安全、审计和合规记录可能会按法律要求或合理业务需要保留更长时间。',
      '您可以通过产品界面的联系方式申请访问、导出、更正或删除账户相关信息。对于依法或为账务、安全、争议处理所必须保留的数据，我们可能无法立即删除。',
    ],
  },
  {
    title: '7. 数据安全',
    body: [
      '我们会采取合理的技术和管理措施保护数据安全，包括加密传输、权限控制、日志审计、异常监控、访问隔离和密钥保护。',
      '互联网传输和第三方服务无法保证绝对安全。因不可抗力、上游服务、第三方系统、用户凭据泄露或用户自行提交敏感信息造成的风险，我们在法律允许范围内不承担责任。',
    ],
  },
  {
    title: '8. 未成年人',
    body: [
      '本服务面向年满 18 周岁、具备完全民事行为能力的成年人、开发者及组织用户，不面向未成年人提供。若您未满 18 周岁，请勿注册或使用本服务。',
      '我们不会有意收集未成年人的个人信息。若您是监护人，发现未成年人在未经您同意的情况下使用本服务或向我们提供了个人信息，请通过产品界面的联系方式与我们联系，我们将依法及时删除相关信息。',
    ],
  },
  {
    title: '9. 跨境传输',
    body: [
      '为实现 API 转发、模型调用、支付结算及基础设施等功能，您的请求内容及必要元数据可能被传输至位于您所在国家或地区境外的上游 AI 服务商、云服务商、支付机构等第三方进行处理。',
      '我们仅在实现上述目的所必需的范围内进行跨境传输，并要求接收方采取相应的数据保护措施。如适用法律要求就跨境传输或敏感信息处理取得您的单独同意，我们将另行以显著方式征得您的同意，您亦有权随时撤回该同意。',
    ],
  },
  {
    title: '10. 政策变更',
    body: [
      '我们可根据业务、法律、技术或安全需要更新本政策。更新后的政策将在网站或产品内发布，并自发布时或指定日期起生效。您继续使用本服务，即表示接受更新后的政策。',
    ],
  },
  {
    title: '11. 联系我们',
    body: [
      '如您对隐私、数据安全或信息处理有任何疑问，请通过产品界面的联系方式与我们联系。',
    ],
  },
];

export default function PrivacyPolicyPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const site = useSiteSettings();
  const { theme, toggleTheme } = useTheme();
  const showStatusEntry = useStatusPageEnabled();
  const isLoggedIn = !!getToken();
  const siteName = site.site_name || 'HopBase';

  return (
    <div className="min-h-screen bg-bg-deep text-text">
      <nav className="sticky top-0 z-20 bg-bg-deep/80 backdrop-blur border-b border-border/50">
        <div className="flex items-center justify-between px-6 md:px-12 py-4 max-w-5xl mx-auto">
          <Link to="/" className="flex items-center gap-2.5">
            <img src={site.site_logo || defaultLogoUrl} alt="" className="w-8 h-8 rounded-sm object-cover" />
            <span className="text-base font-bold">{siteName}</span>
          </Link>
          <div className="flex items-center gap-2">
            {showStatusEntry && (
              <HeroLink
                href="/status"
                className="hidden sm:inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-text-secondary hover:text-text transition-colors"
              >
                <Activity className="w-3.5 h-3.5" />
                {t('nav.status')}
              </HeroLink>
            )}
            <Button
              aria-label={theme === 'dark' ? '切换亮色模式' : '切换暗色模式'}
              isIconOnly
              size="sm"
              variant="ghost"
              onPress={toggleTheme}
            >
              {theme === 'dark' ? <Sun className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
            </Button>
            <Button
              className="ml-1"
              size="sm"
              variant="primary"
              onPress={() => navigate({ to: isLoggedIn ? '/' : '/login' })}
            >
              {isLoggedIn ? t('home.go_dashboard') : t('home.login')}
            </Button>
          </div>
        </div>
      </nav>

      <main className="max-w-5xl mx-auto px-6 md:px-12 py-10 md:py-14">
        <div className="mb-10">
          <div className="inline-flex items-center gap-2 text-xs font-medium text-primary mb-4">
            <ShieldCheck className="w-4 h-4" />
            Privacy
          </div>
          <h1 className="text-3xl md:text-4xl font-bold tracking-normal mb-3">隐私政策</h1>
          <p className="text-sm text-text-tertiary">更新日期：{POLICY_UPDATED_AT}</p>
        </div>

        <section className="border-l-2 border-primary pl-4 md:pl-5 mb-8">
          <p className="text-sm leading-relaxed text-text-secondary">
            本隐私政策说明 {siteName} 在提供 AI API 网关、模型访问、账户、计费和技术支持服务时如何处理必要信息。
            继续注册、登录、创建 API Key、充值或调用 API，即表示您理解并同意本政策。
          </p>
        </section>

        <article className="space-y-8">
          {sections.map((section) => (
            <section key={section.title}>
              <h2 className="text-xl font-bold mb-3 pb-2 border-b border-border">{section.title}</h2>
              <div className="space-y-3">
                {section.body.map((paragraph) => (
                  <p key={paragraph} className="text-[14px] leading-relaxed text-text-secondary">
                    {paragraph}
                  </p>
                ))}
              </div>
            </section>
          ))}
          {site.contact_info ? (
            <section className="border-t border-border pt-5">
              <h2 className="text-base font-semibold mb-2">联系信息</h2>
              <p className="text-sm text-text-secondary whitespace-pre-wrap">{site.contact_info}</p>
            </section>
          ) : null}
        </article>

        <div className="border-t border-border mt-12 pt-8 flex items-center justify-between gap-4">
          <span className="text-sm text-text-tertiary">继续使用即表示您接受本隐私政策。</span>
          <Button variant="primary" onPress={() => navigate({ to: isLoggedIn ? '/' : '/login' })}>
            {isLoggedIn ? t('home.go_dashboard') : t('home.login')}
            <ArrowRight className="w-4 h-4" />
          </Button>
        </div>
      </main>
    </div>
  );
}
