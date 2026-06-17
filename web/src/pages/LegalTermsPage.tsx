import { Link, useNavigate } from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';
import { Button, Link as HeroLink } from '@heroui/react';
import { Activity, ArrowRight, Moon, ShieldCheck, Sun } from 'lucide-react';
import { useSiteSettings, defaultLogoUrl } from '../app/providers/SiteSettingsProvider';
import { useTheme } from '../app/providers/ThemeProvider';
import { useStatusPageEnabled } from '../shared/hooks/useStatusPageEnabled';
import { getToken } from '../shared/api/client';

const TERMS_UPDATED_AT = '2026年6月16日';

const sections = [
  {
    title: '1. 并入的政策',
    body: [
      '本条款与产品页面、价格说明、充值与退款规则、使用规范、支持的模型与地区说明等共同构成您使用本服务时应遵守的完整规则。若相关页面对具体功能、额度、计费或限制有更明确说明，以该等说明为准。',
    ],
  },
  {
    title: '2. 服务说明',
    body: [
      '本服务提供 AI API 网关、模型访问、API Key 管理、用量统计、额度管理、订阅、钱包充值、按量计费、文档与技术支持等能力，帮助开发者以统一入口调用不同 AI 模型或上游服务。',
      '我们主要作为网关、路由与计费层，不直接控制上游模型的生成内容、响应速度、成功率、审核策略、上下文限制、返回结构或价格变动。',
      '具体可用模型、接口能力、上下文长度、速率限制、价格和服务区域，可能随上游服务、成本、风控或产品策略调整而变化。相关变化不构成我们必须继续提供某一模型、某一价格或某一可用性的承诺。',
    ],
  },
  {
    title: '3. 账户与安全',
    body: [
      '您应为年满 18 周岁且具备完全民事行为能力的自然人，或依法设立并有效存续的组织。若您不符合前述条件，请勿注册或使用本服务。',
      '您应提供真实、准确、完整的注册和联系信息，并在信息变化后及时更新。',
      '您应妥善保管账户、密码、API Key、令牌及其他凭据。凡通过您的账户或凭据发起的请求、消费和操作，均视为由您授权或应由您负责。',
      '若发现凭据泄露、异常调用、未授权登录或其他安全事件，请立即在产品内停用相关 Key，并通过产品界面的联系方式通知我们。',
    ],
  },
  {
    title: '4. 费用、订阅与退款',
    body: [
      '费用、钱包余额、订阅权益、赠送额度、优惠券、开票服务费、退款与取消规则，以产品页面、订单页面、支付页面及后台展示为准。',
      '除适用法律另有要求或产品页面另有承诺外，已消耗额度、促销额度、奖励额度以及因违反本条款或滥用服务导致的损失，不按现金价值退还。',
      '由于上游服务商计量、汇率、网络重试、模型路由或风控策略可能变化，账单展示可能存在合理延迟。我们会以系统记录的请求元数据、上游返回和账务流水作为核算依据。',
    ],
  },
  {
    title: '5. 用户行为',
    body: [
      '您须遵守适用法律法规、上游服务提供商政策以及本平台的使用规范。',
      '您不得将本服务用于违法、侵权、欺诈、攻击、绕过访问限制、批量注册滥用、恶意消耗资源、未经授权转售或再分发、生成或传播违法有害内容等行为。',
      '若您的使用造成平台、其他用户、上游服务商或第三方风险，我们有权限制、暂停或终止相关账户、API Key 或请求。',
    ],
  },
  {
    title: '6. 分地区的服务可用性',
    body: [
      '本服务仅面向我们能够合法、稳定提供服务的国家和地区开放。我们可基于法律、制裁、支付、风控、上游服务、网络或业务要求，对部分地区、账户、模型或功能进行限制、暂停或拒绝提供。',
    ],
  },
  {
    title: '7. 知识产权与用户内容',
    body: [
      '本平台的网站、界面、代码、文档、文本、图形、商标及其他内容，由本平台或相关权利人依法享有权利。',
      '您通过 API 提交的输入、文件、上下文以及模型返回内容中的权利，受适用法律和上游服务商政策约束。除为提供、维护和保护本服务所必需的处理外，我们不主张对您的用户内容享有所有权。',
    ],
  },
  {
    title: '8. 第三方服务',
    body: [
      '本服务可能依赖上游 AI 服务、云服务、支付、邮件、对象存储、分析、风控及其他基础设施服务。当您使用依赖此类服务的功能时，相关数据和请求可能会受对应第三方条款、隐私政策和数据处理规则约束。',
      '上游模型的可用性、质量、价格、速率、返回结构和安全策略不完全由我们控制，可能发生变更、中断、限流、封禁或限制。因上游服务故障、接口变更、计费异常、模型拒绝服务或审核拦截导致的影响，不视为我们违约。',
    ],
  },
  {
    title: '9. 隐私与数据安全',
    body: [
      '我们仅保存提供服务所必需的基础信息，例如账户信息、订单记录、余额记录、用量统计、API Key 元数据、登录与安全日志、必要的风控记录等。',
      '我们不会为了训练模型而使用您的 API 请求内容。通常情况下，用户提交给模型的提示词、上下文、上传内容和模型返回内容仅用于完成当次接口转发和响应，不作为平台业务数据长期留存。',
      '为保障计费、风控、安全、防滥用、故障排查和合规要求，系统可能记录必要的请求元数据，例如请求时间、模型名称、Token 或用量、费用、状态码、错误类型、IP、API Key 标识、路由结果等。这些记录通常不包含完整的模型对话正文。',
      '请避免在 API 请求中提交身份证件、银行卡、账户密码、私钥、医疗记录、商业秘密或其他高度敏感信息。因您主动提交此类信息而产生的风险，由您自行承担。',
      '我们会采取合理的技术和管理措施保护用户数据安全，包括权限控制、加密传输、日志审计、异常监测和必要的数据隔离措施。',
    ],
  },
  {
    title: '10. 免责声明与责任限制',
    body: [
      '本服务按“现状”和“可用”基础提供。我们不保证服务永不中断、完全无错误，或满足您的特定业务目标。',
      '我们不对任何模型输出、转发结果、内容审核结果、路由结果、计费结果、余额结算、上下游耗时、第三方可用性或第三方合规决定作出绝对保证。',
      'AI 模型生成的代码、建议、分析或其他输出可能包含事实错误、过时信息、偏见、安全漏洞或知识产权风险。您应自行评估其准确性、安全性和适用性，并在实际使用前进行独立核验。',
      '任何通过本服务获得的结果，仅供开发、测试、辅助或参考用途。您自行决定是否采纳、部署、发布或对外依赖该等结果，并自行承担由此产生的全部风险。',
      'AI 输出不构成医疗、法律、金融、税务、心理健康或其他需要专业资质领域的专业意见。未经合格专业人士审核，您不得将其直接用于相关决策。',
      '在法律允许的范围内，我们对任何间接、附带、特殊、惩罚性或后果性损害不承担责任，包括但不限于利润损失、收入损失、业务中断、数据丢失、模型输出错误、风控拦截、额度误差或商誉损失。',
      '在法律允许的范围内，我们对您承担的责任总额，不超过引发索赔事件前十二个月内您就本服务实际支付的费用总额。',
    ],
  },
  {
    title: '11. 服务变更与终止',
    body: [
      '我们可基于业务、安全、合规或技术原因，修改、暂停或终止全部或部分服务。',
      '如我们认为您违反本条款、相关政策或存在异常风险，我们可限制、暂停或终止您的账户、API Key、模型访问或相关功能。',
      '您可随时停止使用本服务，并可通过产品界面的联系方式申请关闭账户。账户关闭不影响关闭前已产生的费用、账务、合规和安全义务。',
    ],
  },
  {
    title: '12. 赔偿',
    body: [
      '在法律允许的范围内，对于因您违反本条款或相关政策、滥用本服务，或您提交的内容侵犯第三方权利而引发的索赔、损失、罚款、调查、诉讼或合理费用，您应赔偿本平台、关联方及相关人员并使其免受损害。',
    ],
  },
  {
    title: '13. 不可抗力',
    body: [
      '对于超出我们合理控制范围的事件导致的中断、延迟或无法履行，包括自然灾害、战争、政府或监管行动、网络攻击、电力或基础设施中断、上游 AI、支付或云服务故障等，我们不承担责任。我们会在合理范围内尽力恢复服务。',
    ],
  },
  {
    title: '14. 通知',
    body: [
      '我们可通过产品内公告、后台提示、电子邮件、站内消息或您提供的其他联系方式发送通知。除另有说明外，通知在发送或发布时视为送达。',
      '您应保持联系信息准确、完整、及时更新。因未能及时接收通知产生的不利后果，由您自行承担。',
    ],
  },
  {
    title: '15. 权利转让',
    body: [
      '未经我们事先书面同意，您不得将本条款项下的权利或义务转让给任何第三方。我们可在重组、合并、分立、收购或业务调整时，将本条款项下的权利与义务转让给关联方或继受方，并在合理范围内通知您。',
    ],
  },
  {
    title: '16. 可分割性',
    body: [
      '如本条款的任何条款被有管辖权的法院或主管机关认定为无效、违法或不可执行，该条款将在必要的最小范围内修改或删除，其余条款继续有效。',
    ],
  },
  {
    title: '17. 条款变更',
    body: [
      '我们可不时修订本条款。更新后的条款将发布于网站或产品内，并自发布时或指定日期起生效。您在条款更新后继续使用本服务，即表示接受更新后的条款。',
    ],
  },
  {
    title: '18. 适用法律与争议解决',
    body: [
      '本条款适用平台运营方所在地相关法律。因本条款或本服务产生争议的，双方应优先友好协商；协商不成的，可依法通过相应合法途径解决。',
    ],
  },
  {
    title: '19. 联系我们',
    body: [
      '如您对本条款、隐私或数据安全有任何疑问，请通过产品界面的联系方式与我们联系。',
    ],
  },
];

export default function LegalTermsPage() {
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
            Legal
          </div>
          <h1 className="text-3xl md:text-4xl font-bold tracking-normal mb-3">服务条款</h1>
          <p className="text-sm text-text-tertiary">更新日期：{TERMS_UPDATED_AT}</p>
        </div>

        <section className="border-l-2 border-primary pl-4 md:pl-5 mb-8">
          <p className="text-sm leading-relaxed text-text-secondary">
            重要提示：在注册、登录、购买、充值、创建 API Key 或继续使用 {siteName} 之前，
            请您仔细阅读并充分理解本服务条款。只有在您确认能够遵守本条款，并符合您所在国家或地区适用法律的前提下，方可使用本服务。
          </p>
          <p className="mt-3 text-sm leading-relaxed text-text-secondary">
            本服务条款是您与 {siteName} 团队之间就访问和使用本平台网站、管理后台、API 网关、API Key、订阅、钱包、计费及相关服务所达成的协议。
            当您注册、登录、购买、充值、创建 API Key 或以其他方式继续使用本服务时，即表示您已阅读、理解并同意本条款。
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
          <span className="text-sm text-text-tertiary">继续使用即表示您接受上述条款。</span>
          <Button variant="primary" onPress={() => navigate({ to: isLoggedIn ? '/' : '/login' })}>
            {isLoggedIn ? t('home.go_dashboard') : t('home.login')}
            <ArrowRight className="w-4 h-4" />
          </Button>
        </div>
      </main>
    </div>
  );
}
