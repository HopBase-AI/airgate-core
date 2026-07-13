import { lazy } from 'react';
import type { ComponentType, LazyExoticComponent } from 'react';

export type RoutePreloadModule<TProps = Record<string, never>> = {
  default: ComponentType<TProps>;
  preloadUserUsageContent?: () => Promise<unknown>;
};

export type PreloadableLazyComponent<TProps = Record<string, never>> =
  LazyExoticComponent<ComponentType<TProps>> & {
    preload: () => Promise<RoutePreloadModule<TProps>>;
  };

type AnyPreloadableLazyComponent = PreloadableLazyComponent<any>;

export function lazyWithPreload<TProps>(
  load: () => Promise<RoutePreloadModule<TProps>>,
): PreloadableLazyComponent<TProps> {
  let promise: Promise<RoutePreloadModule<TProps>> | undefined;
  const preload = () => {
    promise ??= load();
    return promise;
  };
  const Component = lazy(preload) as PreloadableLazyComponent<TProps>;
  Component.preload = preload;
  return Component;
}

export const SetupPage = lazyWithPreload(() => import('../pages/SetupPage'));
export const LoginPage = lazyWithPreload(() => import('../pages/LoginPage'));
export const PluginPage = lazyWithPreload(() => import('../pages/PluginPage'));
export const DocsPage = lazyWithPreload(() => import('../pages/DocsPage'));
export const LegalTermsPage = lazyWithPreload(() => import('../pages/LegalTermsPage'));
export const PrivacyPolicyPage = lazyWithPreload(() => import('../pages/PrivacyPolicyPage'));
export const DashboardPage = lazyWithPreload(() => import('../pages/DashboardPage'));
export const ModelPlazaPage = lazyWithPreload(() => import('../pages/ModelPlazaPage'));
export const UserOverviewPage = lazyWithPreload(() => import('../pages/user/UserOverviewPage'));
export const UsersPage = lazyWithPreload(() => import('../pages/admin/UsersPage'));
export const AccountsPage = lazyWithPreload(() => import('../pages/admin/AccountsPage'));
export const GroupsPage = lazyWithPreload(() => import('../pages/admin/GroupsPage'));
export const AccountEventsPage = lazyWithPreload(() => import('../pages/admin/AccountEventsPage'));
export const SubscriptionsPage = lazyWithPreload(() => import('../pages/admin/SubscriptionsPage'));
export const ProxiesPage = lazyWithPreload(() => import('../pages/admin/ProxiesPage'));
export const UsagePage = lazyWithPreload(() => import('../pages/admin/UsagePage'));
export const RelayDetectionPage = lazyWithPreload(() => import('../pages/admin/RelayDetectionPage'));
export const PluginsPage = lazyWithPreload(() => import('../pages/admin/PluginsPage'));
export const SettingsPage = lazyWithPreload(() => import('../pages/admin/SettingsPage'));
export const ReferralPage = lazyWithPreload(() => import('../pages/admin/ReferralPage'));
export const ProfilePage = lazyWithPreload(() => import('../pages/user/ProfilePage'));
export const UserKeysPage = lazyWithPreload(() => import('../pages/user/UserKeysPage'));
export const UserUsagePage = lazyWithPreload(() => import('../pages/user/UserUsagePage'));
export const InvitePage = lazyWithPreload(() => import('../pages/user/InvitePage'));

export const ADMIN_IDLE_PRELOADS = [
  DashboardPage,
  PluginPage,
];

export const USER_IDLE_PRELOADS = [
  UserOverviewPage,
  PluginPage,
];

const ROUTE_PRELOADS = new Map<string, AnyPreloadableLazyComponent[]>([
  ['/', [DashboardPage, UserOverviewPage]],
  ['/login', [LoginPage]],
  ['/setup', [SetupPage]],
  ['/docs', [DocsPage]],
  ['/legal/terms', [LegalTermsPage]],
  ['/user-agreement', [LegalTermsPage]],
  ['/legal/privacy', [PrivacyPolicyPage]],
  ['/privacy-policy', [PrivacyPolicyPage]],
  ['/profile', [ProfilePage]],
  ['/keys', [UserKeysPage]],
  ['/usage', [UserUsagePage]],
  ['/invite', [InvitePage]],
  ['/models', [ModelPlazaPage]],
  ['/chat', [PluginPage]],
  ['/studio', [PluginPage]],
  ['/admin/users', [UsersPage]],
  ['/admin/accounts', [AccountsPage]],
  ['/admin/groups', [GroupsPage]],
  ['/admin/account-events', [AccountEventsPage]],
  ['/admin/subscriptions', [SubscriptionsPage]],
  ['/admin/proxies', [ProxiesPage]],
  ['/admin/usage', [UsagePage]],
  ['/admin/relay-detection', [RelayDetectionPage]],
  ['/admin/plugins', [PluginsPage]],
  ['/admin/settings', [SettingsPage]],
  ['/admin/referral', [ReferralPage]],
]);

function normalizePreloadPath(path: string) {
  const [pathname = '/'] = path.split(/[?#]/, 1);
  return pathname || '/';
}

export function preloadRoutePage(
  page: AnyPreloadableLazyComponent,
  options: { deep?: boolean } = {},
) {
  return page.preload().then((module) => (
    options.deep === false ? undefined : module.preloadUserUsageContent?.()
  ));
}

export function preloadRoutePath(path: string, options: { deep?: boolean } = {}) {
  const pathname = normalizePreloadPath(path);
  const pages = pathname.startsWith('/plugins/')
    ? [PluginPage]
    : ROUTE_PRELOADS.get(pathname);

  if (!pages?.length) return Promise.resolve();
  return Promise.all(pages.map((page) => preloadRoutePage(page, options))).then(() => undefined);
}
