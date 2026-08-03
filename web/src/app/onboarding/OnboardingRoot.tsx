import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react';
import { useAuth } from '../providers/AuthProvider';
import { shouldAutoStartOnboarding } from '../../shared/onboarding/storage';
import { ProductTour } from './ProductTour';

interface OnboardingReplayContextValue {
  openGuide: () => void;
}

const OnboardingReplayContext = createContext<OnboardingReplayContextValue>({
  openGuide: () => {},
});

export function OnboardingRoot({ children }: { children: ReactNode }) {
  const { user, loading, isAPIKeySession } = useAuth();
  const [openRequest, setOpenRequest] = useState(0);
  const userId = user?.id ?? 0;
  const disabled = loading || !user || isAPIKeySession;
  const autoStart = useMemo(
    () => !disabled && shouldAutoStartOnboarding(userId),
    [disabled, userId],
  );
  const openGuide = useCallback(() => {
    if (!disabled) setOpenRequest((request) => request + 1);
  }, [disabled]);

  return (
    <OnboardingReplayContext.Provider value={{ openGuide }}>
      {children}
      {!disabled && (
        <ProductTour
          userId={userId}
          autoStart={autoStart}
          openRequest={openRequest}
        />
      )}
    </OnboardingReplayContext.Provider>
  );
}

export function useOnboardingReplay() {
  return useContext(OnboardingReplayContext);
}
