import { describe, expect, it } from 'vitest';
import { isModelRouteSelected, normalizeModelRouting, toggleModelRoute } from './modelRouting';

describe('group model routing controls', () => {
  it('treats an empty account route as unselected', () => {
    expect(isModelRouteSelected({ 'gpt-5.6': [] }, 'gpt-5.6')).toBe(false);
    expect(isModelRouteSelected({ 'gpt-5.6': [18] }, 'gpt-5.6')).toBe(true);
  });

  it('fills an empty route with unique default accounts when selected', () => {
    const original = { 'gpt-5.6': [] };
    const updated = toggleModelRoute(original, 'gpt-5.6', true, [18, 47, 18]);

    expect(updated).toEqual({ 'gpt-5.6': [18, 47] });
    expect(original).toEqual({ 'gpt-5.6': [] });
  });

  it('keeps an existing route when selected and removes it when unselected', () => {
    const original = { 'gpt-5.6': [18] };

    expect(toggleModelRoute(original, 'gpt-5.6', true, [47])).toEqual(original);
    expect(toggleModelRoute(original, 'gpt-5.6', false, [47])).toEqual({});
    expect(original).toEqual({ 'gpt-5.6': [18] });
  });

  it('removes stale empty routes before submitting the form', () => {
    const original = { 'gpt-5.6': [], 'gpt-5.6-sol': [18, 18, 47] };

    expect(normalizeModelRouting(original)).toEqual({ 'gpt-5.6-sol': [18, 47] });
    expect(original).toEqual({ 'gpt-5.6': [], 'gpt-5.6-sol': [18, 18, 47] });
  });
});
