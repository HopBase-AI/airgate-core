import { describe, expect, it, vi } from 'vitest';
import {
  findOnboardingTarget,
  getTourSteps,
  runOnboardingStepAction,
} from './steps';

describe('onboarding tour steps', () => {
  it('keeps the developer flow on keys and ends with centered guidance', () => {
    const steps = getTourSteps('developer');

    expect(steps).toHaveLength(4);
    expect(steps.every((step) => step.route === '/keys')).toBe(true);
    expect(steps[1]?.action).toBe('open-create-key');
    expect(steps[1]?.nextRequiresSelector).toBe(
      '[data-onboarding-target="key-actions"]',
    );
    expect(steps[2]?.action).toBe('open-key-actions');
    expect(steps[2]?.prerequisiteSelector).toBe(
      '[data-onboarding-target="key-actions"]',
    );
    expect(steps[3]?.centered).toBe(true);
  });

  it('switches local Studio media state before image and video option steps', () => {
    const steps = getTourSteps('creator');

    expect(steps).toHaveLength(5);
    expect(steps[2]?.action).toBe('select-image');
    expect(steps[3]?.action).toBe('select-video');
    expect(steps[4]?.targetSelectors).toEqual([
      '[data-onboarding-target="studio-generate"]',
    ]);
  });

  it('uses the first available fallback target', () => {
    const fallback = { id: 'fallback' } as HTMLElement;
    const querySelector = vi.fn((selector: string) => (
      selector === '[data-onboarding-target="keys-create"]' ? fallback : null
    ));
    const root = { querySelector } as unknown as Pick<Document, 'querySelector'>;

    expect(findOnboardingTarget(root, [
      '[data-onboarding-target="key-actions"]',
      '[data-onboarding-target="keys-create"]',
    ])).toBe(fallback);
    expect(querySelector).toHaveBeenCalledTimes(2);
  });

  it('retries a step action until its lazy target is available', () => {
    const click = vi.fn();
    const querySelector = vi
      .fn()
      .mockReturnValueOnce(null)
      .mockReturnValueOnce({ click });
    const root = { querySelector } as unknown as Pick<Document, 'querySelector'>;

    expect(runOnboardingStepAction(root, 'select-video')).toBe(false);
    expect(runOnboardingStepAction(root, 'select-video')).toBe(true);
    expect(click).toHaveBeenCalledTimes(1);
  });

  it('does not reopen an already visible key actions menu', () => {
    const querySelector = vi.fn(() => ({ id: 'ccs-import' } as HTMLElement));
    const root = { querySelector } as unknown as Pick<Document, 'querySelector'>;

    expect(runOnboardingStepAction(root, 'open-key-actions')).toBe(true);
    expect(querySelector).toHaveBeenCalledTimes(1);
  });

  it('opens the create-key form without submitting it', () => {
    const click = vi.fn();
    const trigger = {
      click,
      getAttribute: vi.fn(() => null),
      matches: vi.fn(() => false),
    } as unknown as HTMLElement;
    const querySelector = vi.fn((selector: string) => (
      selector === '[data-onboarding-target="keys-create"]' ? trigger : null
    ));
    const root = { querySelector } as unknown as Pick<Document, 'querySelector'>;

    expect(runOnboardingStepAction(root, 'open-create-key')).toBe(true);
    expect(click).toHaveBeenCalledTimes(1);
  });

  it('does not click a disabled create-key control', () => {
    const click = vi.fn();
    const trigger = {
      click,
      getAttribute: vi.fn(() => 'true'),
      matches: vi.fn(() => true),
    } as unknown as HTMLElement;
    const querySelector = vi.fn((selector: string) => (
      selector === '[data-onboarding-target="keys-create"]' ? trigger : null
    ));
    const root = { querySelector } as unknown as Pick<Document, 'querySelector'>;

    expect(runOnboardingStepAction(root, 'open-create-key')).toBe(false);
    expect(click).not.toHaveBeenCalled();
  });
});
