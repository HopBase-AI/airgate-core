import { describe, expect, it } from 'vitest';
import { generationDurationSeconds, hasUpstreamTiming } from './GenerationTasksPage';
import type { GenerationTaskResp } from '../../shared/types';

function task(overrides: Partial<GenerationTaskResp> = {}): GenerationTaskResp {
  return {
    id: 1,
    plugin_id: 'airgate-seedance',
    task_type: 'video.api',
    kind: 'video',
    status: 'completed',
    user_id: 1,
    progress: 100,
    attempts: 1,
    max_attempts: 1000,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:10:00Z',
    completed_at: '2026-08-01T00:10:00Z',
    ...overrides,
  };
}

describe('generation task duration', () => {
  it('prefers complete upstream timestamps', () => {
    const item = task({
      upstream_created_at: '2026-08-01T00:01:00Z',
      upstream_completed_at: '2026-08-01T00:04:30Z',
    });
    expect(hasUpstreamTiming(item)).toBe(true);
    expect(generationDurationSeconds(item)).toBe(210);
  });

  it('falls back to local creation and completion for historical tasks', () => {
    const item = task();
    expect(hasUpstreamTiming(item)).toBe(false);
    expect(generationDurationSeconds(item)).toBe(600);
  });

  it('measures an active upstream task through the current time', () => {
    const item = task({
      status: 'processing',
      completed_at: undefined,
      upstream_created_at: '2026-08-01T00:02:00Z',
    });
    expect(hasUpstreamTiming(item)).toBe(true);
    expect(generationDurationSeconds(item, Date.parse('2026-08-01T00:05:00Z'))).toBe(180);
  });
});
