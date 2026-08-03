import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
} from 'react';
import { createPortal } from 'react-dom';
import { useNavigate, useRouterState } from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';
import { Button, Modal, useOverlayState } from '@heroui/react';
import { Code2, Images } from 'lucide-react';
import { DialogTriggerShim } from '../../shared/components/DialogTriggerShim';
import {
  clearActiveTourPath,
  clearNewRegistrationMarker,
  hasNewRegistrationMarker,
  readActiveTourPath,
  readOnboardingRecord,
  writeActiveTourPath,
  writeOnboardingRecord,
  type OnboardingPath,
  type OnboardingStatus,
} from '../../shared/onboarding/storage';
import {
  findOnboardingTarget,
  getTourSteps,
  runOnboardingStepAction,
} from '../../shared/onboarding/steps';

export interface ProductTourProps {
  userId: number;
  autoStart: boolean;
  openRequest: number;
  disabled?: boolean;
}

type TourPhase = 'closed' | 'choice' | 'path' | 'tour';
type LaunchMode = 'auto' | 'manual' | 'resume';

interface ViewportSize {
  width: number;
  height: number;
}

interface SpotlightRect {
  top: number;
  left: number;
  right: number;
  bottom: number;
  width: number;
  height: number;
}

const TARGET_PADDING = 8;
const TARGET_RETRY_MS = 120;
const TARGET_TIMEOUT_MS = 4800;
const PANEL_GAP = 16;
const VIEWPORT_INSET = 12;

function currentViewport(): ViewportSize {
  if (typeof window === 'undefined') return { width: 1024, height: 768 };
  return { width: window.innerWidth, height: window.innerHeight };
}

function toSpotlightRect(target: HTMLElement, viewport: ViewportSize): SpotlightRect {
  const rect = target.getBoundingClientRect();
  const left = Math.max(0, rect.left - TARGET_PADDING);
  const top = Math.max(0, rect.top - TARGET_PADDING);
  const right = Math.min(viewport.width, rect.right + TARGET_PADDING);
  const bottom = Math.min(viewport.height, rect.bottom + TARGET_PADDING);
  return {
    top,
    left,
    right,
    bottom,
    width: Math.max(0, right - left),
    height: Math.max(0, bottom - top),
  };
}

function sameRect(left: SpotlightRect | null, right: SpotlightRect): boolean {
  return !!left
    && Math.abs(left.top - right.top) < 0.5
    && Math.abs(left.left - right.left) < 0.5
    && Math.abs(left.width - right.width) < 0.5
    && Math.abs(left.height - right.height) < 0.5;
}

function panelPosition(
  rect: SpotlightRect | null,
  panelSize: { width: number; height: number },
  viewport: ViewportSize,
  centered: boolean,
): CSSProperties {
  const width = Math.min(360, viewport.width - VIEWPORT_INSET * 2);
  if (viewport.width < 640) {
    return {
      left: VIEWPORT_INSET,
      bottom: `calc(${VIEWPORT_INSET}px + env(safe-area-inset-bottom))`,
      width,
    };
  }

  if (!rect || centered) {
    return {
      left: Math.max(VIEWPORT_INSET, (viewport.width - width) / 2),
      top: Math.max(VIEWPORT_INSET, (viewport.height - panelSize.height) / 2),
      width,
    };
  }

  const panelHeight = panelSize.height || 280;
  const below = rect.bottom + PANEL_GAP;
  const above = rect.top - PANEL_GAP - panelHeight;
  let top: number;
  let left = Math.min(
    Math.max(VIEWPORT_INSET, rect.left),
    viewport.width - width - VIEWPORT_INSET,
  );

  if (below + panelHeight <= viewport.height - VIEWPORT_INSET) {
    top = below;
  } else if (above >= VIEWPORT_INSET) {
    top = above;
  } else {
    const rightSpace = viewport.width - rect.right;
    const leftSpace = rect.left;
    top = Math.min(
      Math.max(VIEWPORT_INSET, rect.top),
      viewport.height - panelHeight - VIEWPORT_INSET,
    );
    if (rightSpace >= width + PANEL_GAP) left = rect.right + PANEL_GAP;
    else if (leftSpace >= width + PANEL_GAP) left = rect.left - PANEL_GAP - width;
  }

  return { left, top, width };
}

function OnboardingChooser({
  phase,
  onClose,
  onNovice,
  onExperienced,
  onSelectPath,
}: {
  phase: 'choice' | 'path';
  onClose: () => void;
  onNovice: () => void;
  onExperienced: () => void;
  onSelectPath: (path: OnboardingPath) => void;
}) {
  const { t } = useTranslation();
  const state = useOverlayState({
    isOpen: true,
    onOpenChange: (open) => {
      if (!open) onClose();
    },
  });

  return (
    <Modal state={state}>
      <DialogTriggerShim />
      <Modal.Backdrop className="ag-onboarding-modal-backdrop">
        <Modal.Container placement="center" size="sm">
          <Modal.Dialog className="ag-onboarding-chooser">
            <Modal.Header>
              <Modal.Heading>
                {phase === 'choice' ? t('onboarding.welcome_title') : t('onboarding.path_title')}
              </Modal.Heading>
              <Modal.CloseTrigger />
            </Modal.Header>
            <Modal.Body>
              {phase === 'choice' ? (
                <p className="ag-onboarding-chooser-copy">{t('onboarding.welcome_body')}</p>
              ) : (
                <div className="ag-onboarding-paths">
                  <p className="ag-onboarding-chooser-copy">{t('onboarding.path_body')}</p>
                  <Button
                    className="ag-onboarding-path-action"
                    variant="secondary"
                    onPress={() => onSelectPath('developer')}
                  >
                    <Code2 aria-hidden="true" className="h-5 w-5 shrink-0" />
                    <span>
                      <strong>{t('onboarding.developer_title')}</strong>
                      <small>{t('onboarding.developer_body')}</small>
                    </span>
                  </Button>
                  <Button
                    className="ag-onboarding-path-action"
                    variant="secondary"
                    onPress={() => onSelectPath('creator')}
                  >
                    <Images aria-hidden="true" className="h-5 w-5 shrink-0" />
                    <span>
                      <strong>{t('onboarding.creator_title')}</strong>
                      <small>{t('onboarding.creator_body')}</small>
                    </span>
                  </Button>
                </div>
              )}
            </Modal.Body>
            {phase === 'choice' && (
              <Modal.Footer className="ag-onboarding-choice-footer">
                <Button variant="ghost" onPress={onExperienced}>
                  {t('onboarding.experienced_action')}
                </Button>
                <Button variant="primary" onPress={onNovice}>
                  {t('onboarding.novice_action')}
                </Button>
              </Modal.Footer>
            )}
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}

function TourBackdrop({ rect }: { rect: SpotlightRect | null }) {
  if (!rect) return <div className="ag-onboarding-backdrop ag-onboarding-backdrop-full" />;
  return (
    <>
      <div className="ag-onboarding-backdrop" style={{ inset: `0 0 auto 0`, height: rect.top }} />
      <div
        className="ag-onboarding-backdrop"
        style={{ left: 0, top: rect.top, width: rect.left, height: rect.height }}
      />
      <div
        className="ag-onboarding-backdrop"
        style={{ left: rect.right, right: 0, top: rect.top, height: rect.height }}
      />
      <div className="ag-onboarding-backdrop" style={{ inset: `${rect.bottom}px 0 0 0` }} />
      <div
        aria-hidden="true"
        className="ag-onboarding-spotlight"
        style={{ left: rect.left, top: rect.top, width: rect.width, height: rect.height }}
      />
    </>
  );
}

export function ProductTour({ userId, autoStart, openRequest, disabled = false }: ProductTourProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const routerPath = useRouterState({ select: (state) => state.location.pathname });
  const [phase, setPhase] = useState<TourPhase>('closed');
  const [path, setPath] = useState<OnboardingPath | null>(null);
  const [stepIndex, setStepIndex] = useState(0);
  const [targetRect, setTargetRect] = useState<SpotlightRect | null>(null);
  const [resolving, setResolving] = useState(false);
  const [targetMissing, setTargetMissing] = useState(false);
  const [pluginUnavailable, setPluginUnavailable] = useState(false);
  const [nextRequirementMet, setNextRequirementMet] = useState(true);
  const [prerequisiteMissing, setPrerequisiteMissing] = useState(false);
  const [keyGroupsUnavailable, setKeyGroupsUnavailable] = useState(false);
  const [keyAvailable, setKeyAvailable] = useState(false);
  const [viewport, setViewport] = useState<ViewportSize>(currentViewport);
  const [panelSize, setPanelSize] = useState({ width: 360, height: 280 });
  const panelRef = useRef<HTMLDivElement>(null);
  const headingRef = useRef<HTMLHeadingElement>(null);
  const initializedUserRef = useRef<number | null>(null);
  const lastOpenRequestRef = useRef(openRequest);
  const launchModeRef = useRef<LaunchMode>('auto');
  const returnFocusRef = useRef<HTMLElement | null>(null);
  const returnRouteRef = useRef('/');
  const steps = useMemo(() => (path ? getTourSteps(path) : []), [path]);
  const step = steps[stepIndex];

  const restoreReplayFocus = useCallback(() => {
    if (launchModeRef.current !== 'manual') return;
    const focusReplay = () => {
      const original = returnFocusRef.current;
      if (original?.isConnected) {
        original.focus();
        return;
      }
      document.querySelector<HTMLElement>('[data-onboarding-replay="true"]')?.focus();
    };
    if (routerPath !== returnRouteRef.current) {
      void navigate({ to: returnRouteRef.current }).then(() => {
        window.setTimeout(focusReplay, 0);
      });
      return;
    }
    window.setTimeout(focusReplay, 0);
  }, [navigate, routerPath]);

  const closeTour = useCallback((status?: OnboardingStatus) => {
    if (userId > 0) {
      clearNewRegistrationMarker(userId);
      clearActiveTourPath(userId);
      if (status) {
        const existing = readOnboardingRecord(userId);
        const preserveCompleted = launchModeRef.current === 'manual' && existing?.status === 'completed';
        if (!preserveCompleted) writeOnboardingRecord(userId, status, path ?? undefined);
      }
    }
    setPhase('closed');
    setPath(null);
    setStepIndex(0);
    setTargetRect(null);
    setResolving(false);
    setTargetMissing(false);
    setPluginUnavailable(false);
    setNextRequirementMet(true);
    setPrerequisiteMissing(false);
    setKeyGroupsUnavailable(false);
    setKeyAvailable(false);
    restoreReplayFocus();
  }, [path, restoreReplayFocus, userId]);

  useEffect(() => {
    if (disabled) {
      initializedUserRef.current = null;
      return;
    }
    if (routerPath === '/login' || initializedUserRef.current === userId) return;
    const initialize = window.setTimeout(() => {
      initializedUserRef.current = userId;
      const activePath = readActiveTourPath(userId);
      if (activePath) {
        launchModeRef.current = 'resume';
        setPath(activePath);
        setStepIndex(0);
        setResolving(true);
        setPhase('tour');
        return;
      }
      if (autoStart && hasNewRegistrationMarker(userId)) {
        launchModeRef.current = 'auto';
        setPhase('choice');
      }
    }, 0);
    return () => window.clearTimeout(initialize);
  }, [autoStart, disabled, routerPath, userId]);

  useEffect(() => {
    if (openRequest === lastOpenRequestRef.current) return;
    lastOpenRequestRef.current = openRequest;
    if (disabled) return;
    returnFocusRef.current = document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null;
    returnRouteRef.current = routerPath;
    launchModeRef.current = 'manual';
    clearActiveTourPath(userId);
    const open = window.setTimeout(() => {
      setPath(null);
      setStepIndex(0);
      setPhase('path');
    }, 0);
    return () => window.clearTimeout(open);
  }, [disabled, openRequest, routerPath, userId]);

  useEffect(() => {
    if (phase !== 'tour' || !step || routerPath === step.route) return;
    void navigate({ to: step.route });
  }, [navigate, phase, routerPath, step]);

  useEffect(() => {
    if (phase !== 'tour') return;
    const updateViewport = () => setViewport(currentViewport());
    window.addEventListener('resize', updateViewport);
    return () => window.removeEventListener('resize', updateViewport);
  }, [phase]);

  useEffect(() => {
    if (phase !== 'tour' || !step) return;
    if (routerPath !== step.route) return;
    if (step.centered || step.targetSelectors.length === 0) {
      const center = window.setTimeout(() => {
        setResolving(false);
        setTargetMissing(false);
        setPluginUnavailable(false);
        setNextRequirementMet(true);
        setPrerequisiteMissing(false);
        setKeyGroupsUnavailable(false);
        setKeyAvailable(false);
        setTargetRect(null);
      }, 0);
      return () => window.clearTimeout(center);
    }

    let disposed = false;
    let actionRun = false;
    let scrolled = false;
    const startedAt = Date.now();
    const locate = () => {
      if (disposed) return;
      const prerequisiteReady = !step.prerequisiteSelector
        || !!document.querySelector(step.prerequisiteSelector);
      const requirementMet = !step.nextRequiresSelector
        || !!document.querySelector(step.nextRequiresSelector);
      const hasKey = !!document.querySelector('[data-onboarding-target="key-actions"]');
      const createControl = document.querySelector<HTMLElement>(
        '[data-onboarding-target="keys-create"]',
      );
      setPrerequisiteMissing(!prerequisiteReady);
      setNextRequirementMet(requirementMet);
      setKeyAvailable(hasKey);
      setKeyGroupsUnavailable(
        path === 'developer'
        && createControl?.dataset.onboardingAvailable === 'false',
      );
      if (!actionRun) {
        actionRun = runOnboardingStepAction(document, step.action);
      }
      const target = findOnboardingTarget(document, step.targetSelectors);
      if (target) {
        if (!scrolled) {
          const rect = target.getBoundingClientRect();
          if (rect.top < 12 || rect.bottom > window.innerHeight - 12) {
            const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
            target.scrollIntoView({ block: 'center', behavior: reducedMotion ? 'auto' : 'smooth' });
          }
          scrolled = true;
        }
        const nextViewport = currentViewport();
        const nextRect = toSpotlightRect(target, nextViewport);
        setViewport((current) => (
          current.width === nextViewport.width && current.height === nextViewport.height
            ? current
            : nextViewport
        ));
        setTargetRect((current) => (sameRect(current, nextRect) ? current : nextRect));
        setResolving(false);
        setTargetMissing(false);
        setPluginUnavailable(false);
        return;
      }
      setTargetRect(null);
      if (Date.now() - startedAt >= TARGET_TIMEOUT_MS) {
        setResolving(false);
        setTargetMissing(true);
        setPluginUnavailable(
          path === 'creator'
          && !document.querySelector('[data-onboarding-target^="studio-"]'),
        );
      }
    };

    const initialLocate = window.setTimeout(locate, 0);
    const interval = window.setInterval(locate, TARGET_RETRY_MS);
    window.addEventListener('resize', locate);
    window.addEventListener('scroll', locate, true);
    return () => {
      disposed = true;
      window.clearTimeout(initialLocate);
      window.clearInterval(interval);
      window.removeEventListener('resize', locate);
      window.removeEventListener('scroll', locate, true);
    };
  }, [path, phase, routerPath, step]);

  useLayoutEffect(() => {
    if (phase !== 'tour' || !panelRef.current) return;
    const panel = panelRef.current;
    const measure = () => {
      const rect = panel.getBoundingClientRect();
      setPanelSize((current) => (
        Math.abs(current.width - rect.width) < 0.5 && Math.abs(current.height - rect.height) < 0.5
          ? current
          : { width: rect.width, height: rect.height }
      ));
    };
    measure();
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(measure);
    observer?.observe(panel);
    return () => observer?.disconnect();
  }, [
    keyGroupsUnavailable,
    keyAvailable,
    nextRequirementMet,
    phase,
    prerequisiteMissing,
    resolving,
    stepIndex,
    targetMissing,
  ]);

  useEffect(() => {
    if (phase !== 'tour') return;
    const focusHeading = window.requestAnimationFrame(() => headingRef.current?.focus());
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        closeTour('dismissed');
        return;
      }
      if (event.key !== 'Tab' || !panelRef.current) return;
      const focusable = Array.from(panelRef.current.querySelectorAll<HTMLElement>(
        'button:not([disabled]), [href], input:not([disabled]), [tabindex]:not([tabindex="-1"])',
      ));
      if (focusable.length === 0) {
        event.preventDefault();
        headingRef.current?.focus();
        return;
      }
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      const activeElement = document.activeElement;
      if (!panelRef.current.contains(activeElement)) {
        event.preventDefault();
        (event.shiftKey ? last : first)?.focus();
      } else if (event.shiftKey && (activeElement === first || activeElement === headingRef.current)) {
        event.preventDefault();
        last?.focus();
      } else if (!event.shiftKey && activeElement === last) {
        event.preventDefault();
        first?.focus();
      }
    };
    document.addEventListener('keydown', onKeyDown);
    return () => {
      window.cancelAnimationFrame(focusHeading);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [closeTour, phase, stepIndex]);

  const startPath = (nextPath: OnboardingPath) => {
    writeActiveTourPath(userId, nextPath);
    setPath(nextPath);
    setStepIndex(0);
    setTargetRect(null);
    setTargetMissing(false);
    setPluginUnavailable(false);
    setNextRequirementMet(true);
    setPrerequisiteMissing(false);
    setKeyGroupsUnavailable(false);
    setKeyAvailable(false);
    setResolving(true);
    setPhase('tour');
  };

  const moveToStep = (nextStep: number) => {
    setTargetRect(null);
    setTargetMissing(false);
    setPluginUnavailable(false);
    setNextRequirementMet(true);
    setPrerequisiteMissing(false);
    setKeyGroupsUnavailable(false);
    setKeyAvailable(false);
    setResolving(true);
    setStepIndex(nextStep);
  };

  const finish = () => closeTour('completed');
  const destination = step?.route === '/studio'
    ? t('onboarding.destination_studio')
    : t('onboarding.destination_keys');
  const routeChanging = routerPath !== step?.route;
  const centered = !!step?.centered || routeChanging || resolving || targetMissing || !targetRect;
  const visibleTargetRect = centered ? null : targetRect;

  if (disabled || phase === 'closed') return null;
  if (phase === 'choice' || phase === 'path') {
    return (
      <OnboardingChooser
        phase={phase}
        onClose={() => closeTour('dismissed')}
        onNovice={() => setPhase('path')}
        onExperienced={() => closeTour('skipped')}
        onSelectPath={startPath}
      />
    );
  }
  if (!step || !path) return null;

  const isFinal = stepIndex === steps.length - 1;
  const showPluginFallback = pluginUnavailable && targetMissing;
  const showNoGroupsFallback = path === 'developer'
    && keyGroupsUnavailable
    && !keyAvailable
    && !step.centered;
  const showPrerequisiteFallback = !showNoGroupsFallback
    && !!step.prerequisiteSelector
    && prerequisiteMissing;
  const showCompletionCopy = !showNoGroupsFallback
    && !showPrerequisiteFallback
    && !!step.nextRequiresSelector
    && nextRequirementMet;
  const nextBlocked = !showPluginFallback && (
    showNoGroupsFallback
    || showPrerequisiteFallback
    || (!!step.nextRequiresSelector && !nextRequirementMet)
  );
  const title = showPluginFallback
    ? t('onboarding.plugin_unavailable_title')
    : showNoGroupsFallback
      ? t('onboarding.no_groups_title')
      : showPrerequisiteFallback
        ? t(step.prerequisiteTitleKey ?? step.titleKey)
        : showCompletionCopy
          ? t(step.completionTitleKey ?? step.titleKey)
          : t(step.titleKey);
  const body = showPluginFallback
    ? t('onboarding.plugin_unavailable_body')
    : showNoGroupsFallback
      ? t('onboarding.no_groups_body')
      : showPrerequisiteFallback
        ? t(step.prerequisiteBodyKey ?? step.bodyKey)
        : showCompletionCopy
          ? t(step.completionBodyKey ?? step.bodyKey)
          : t(step.bodyKey);
  const position = panelPosition(visibleTargetRect, panelSize, viewport, centered);
  const panel = (
    <div className="ag-onboarding-layer">
      <TourBackdrop rect={visibleTargetRect} />
      <div
        ref={panelRef}
        className="ag-onboarding-panel"
        style={position}
        role="dialog"
        aria-modal="true"
        aria-labelledby="ag-onboarding-step-title"
        aria-describedby="ag-onboarding-step-body"
      >
        <div className="ag-onboarding-progress-copy">
          {t('onboarding.step_count', { current: stepIndex + 1, total: steps.length })}
        </div>
        <div className="ag-onboarding-progress" aria-hidden="true">
          <span style={{ width: `${((stepIndex + 1) / steps.length) * 100}%` }} />
        </div>
        <h2
          ref={headingRef}
          id="ag-onboarding-step-title"
          className="ag-onboarding-step-title"
          tabIndex={-1}
        >
          {title}
        </h2>
        <div id="ag-onboarding-step-body" className="ag-onboarding-step-body">
          <p>{body}</p>
          {(resolving || routeChanging) && (
            <p className="ag-onboarding-state-copy">{t('onboarding.loading')}</p>
          )}
          {!!step.nextRequiresSelector
            && !nextRequirementMet
            && !showNoGroupsFallback
            && !showPrerequisiteFallback && (
            <p className="ag-onboarding-state-copy">{t('onboarding.complete_before_next')}</p>
          )}
          {targetMissing && !showPluginFallback && !nextBlocked && (
            <p className="ag-onboarding-state-copy">
              {t('onboarding.target_missing', { destination })}
            </p>
          )}
        </div>
        <div className="ag-onboarding-footer">
          <Button
            size="sm"
            variant="secondary"
            isDisabled={!showPluginFallback && stepIndex === 0}
            onPress={() => {
              if (showPluginFallback) {
                clearActiveTourPath(userId);
                setPath(null);
                setStepIndex(0);
                setPhase('path');
              } else {
                moveToStep(Math.max(0, stepIndex - 1));
              }
            }}
          >
            {showPluginFallback ? t('onboarding.back') : t('onboarding.previous')}
          </Button>
          {!showPluginFallback && (
            <Button size="sm" variant="ghost" onPress={() => closeTour('dismissed')}>
              {t('onboarding.skip')}
            </Button>
          )}
          <Button
            className="ag-onboarding-next"
            size="sm"
            variant="primary"
            isDisabled={nextBlocked}
            onPress={() => {
              if (showPluginFallback || isFinal) finish();
              else moveToStep(Math.min(steps.length - 1, stepIndex + 1));
            }}
          >
            {showPluginFallback || isFinal ? t('onboarding.finish') : t('onboarding.next')}
          </Button>
        </div>
      </div>
      <div className="ag-onboarding-live" aria-live="polite" aria-atomic="true">
        {t('onboarding.step_announcement', { current: stepIndex + 1, total: steps.length })}
      </div>
    </div>
  );

  return createPortal(panel, document.body);
}
