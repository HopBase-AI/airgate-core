import type { OnboardingPath } from './storage';

export type TourStepAction =
  | 'open-create-key'
  | 'open-key-actions'
  | 'select-image'
  | 'select-video';

export interface TourStepDefinition {
  id: string;
  route: '/keys' | '/studio';
  targetSelectors: string[];
  titleKey: string;
  bodyKey: string;
  action?: TourStepAction;
  centered?: boolean;
  nextRequiresSelector?: string;
  completionTitleKey?: string;
  completionBodyKey?: string;
  prerequisiteSelector?: string;
  prerequisiteTitleKey?: string;
  prerequisiteBodyKey?: string;
}

const DEVELOPER_STEPS: TourStepDefinition[] = [
  {
    id: 'developer-create-key',
    route: '/keys',
    targetSelectors: ['[data-onboarding-target="keys-create"]'],
    titleKey: 'onboarding.steps.developer.create_title',
    bodyKey: 'onboarding.steps.developer.create_body',
  },
  {
    id: 'developer-configure-key',
    route: '/keys',
    targetSelectors: [
      '[data-onboarding-target="key-create-group"]',
      '[data-onboarding-target="key-actions"]',
      '[data-onboarding-target="keys-create"]',
    ],
    titleKey: 'onboarding.steps.developer.configure_title',
    bodyKey: 'onboarding.steps.developer.configure_body',
    action: 'open-create-key',
    nextRequiresSelector: '[data-onboarding-target="key-actions"]',
    completionTitleKey: 'onboarding.steps.developer.created_title',
    completionBodyKey: 'onboarding.steps.developer.created_body',
  },
  {
    id: 'developer-import-ccs',
    route: '/keys',
    targetSelectors: [
      '[data-onboarding-target="ccs-import"]',
      '[data-onboarding-target="key-actions"]',
      '[data-onboarding-target="keys-create"]',
    ],
    titleKey: 'onboarding.steps.developer.import_title',
    bodyKey: 'onboarding.steps.developer.import_body',
    action: 'open-key-actions',
    prerequisiteSelector: '[data-onboarding-target="key-actions"]',
    prerequisiteTitleKey: 'onboarding.steps.developer.create_required_title',
    prerequisiteBodyKey: 'onboarding.steps.developer.create_required_body',
  },
  {
    id: 'developer-enable-provider',
    route: '/keys',
    targetSelectors: [],
    titleKey: 'onboarding.steps.developer.enable_title',
    bodyKey: 'onboarding.steps.developer.enable_body',
    centered: true,
  },
];

const CREATOR_STEPS: TourStepDefinition[] = [
  {
    id: 'creator-media',
    route: '/studio',
    targetSelectors: ['[data-onboarding-target="studio-media"]'],
    titleKey: 'onboarding.steps.creator.media_title',
    bodyKey: 'onboarding.steps.creator.media_body',
  },
  {
    id: 'creator-prompt',
    route: '/studio',
    targetSelectors: ['[data-onboarding-target="studio-prompt"]'],
    titleKey: 'onboarding.steps.creator.prompt_title',
    bodyKey: 'onboarding.steps.creator.prompt_body',
  },
  {
    id: 'creator-image-options',
    route: '/studio',
    targetSelectors: ['[data-onboarding-target="studio-image-options"]'],
    titleKey: 'onboarding.steps.creator.image_options_title',
    bodyKey: 'onboarding.steps.creator.image_options_body',
    action: 'select-image',
  },
  {
    id: 'creator-video-options',
    route: '/studio',
    targetSelectors: ['[data-onboarding-target="studio-video-params"]'],
    titleKey: 'onboarding.steps.creator.video_options_title',
    bodyKey: 'onboarding.steps.creator.video_options_body',
    action: 'select-video',
  },
  {
    id: 'creator-generate',
    route: '/studio',
    targetSelectors: ['[data-onboarding-target="studio-generate"]'],
    titleKey: 'onboarding.steps.creator.generate_title',
    bodyKey: 'onboarding.steps.creator.generate_body',
  },
];

export function getTourSteps(path: OnboardingPath): readonly TourStepDefinition[] {
  return path === 'developer' ? DEVELOPER_STEPS : CREATOR_STEPS;
}

export function findOnboardingTarget(
  root: Pick<Document, 'querySelector'>,
  selectors: readonly string[],
): HTMLElement | null {
  for (const selector of selectors) {
    const target = root.querySelector<HTMLElement>(selector);
    if (target) return target;
  }
  return null;
}

export function runOnboardingStepAction(
  root: Pick<Document, 'querySelector'>,
  action: TourStepAction | undefined,
): boolean {
  if (!action) return true;

  if (action === 'open-create-key') {
    if (
      root.querySelector('[data-onboarding-target="key-actions"]')
      || root.querySelector('[data-onboarding-target="key-create-group"]')
    ) {
      return true;
    }
    const trigger = root.querySelector<HTMLElement>(
      '[data-onboarding-target="keys-create"]',
    );
    if (
      !trigger
      || trigger.matches(':disabled')
      || trigger.getAttribute('aria-disabled') === 'true'
    ) {
      return false;
    }
    trigger.click();
    return true;
  }

  if (action === 'open-key-actions') {
    if (root.querySelector('[data-onboarding-target="ccs-import"]')) return true;
    const trigger = root.querySelector<HTMLElement>(
      '[data-onboarding-target="key-actions"]',
    );
    if (!trigger) return false;
    trigger.click();
    return true;
  }

  const targetName = action === 'select-video' ? 'studio-video' : 'studio-image';
  const control = root.querySelector<HTMLElement>(
    `[data-onboarding-target="${targetName}"]`,
  );
  if (!control) return false;
  control.click();
  return true;
}
