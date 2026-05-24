/**
 * Tests — useSyncStatus hook — conflict toast behavior (v2.2 #144)
 *
 * Couvre :
 *   - listener sync:conflict → toast individuel affiché
 *   - debounce 500 ms par path → deuxième événement identique ignoré
 *   - collapse > 3 conflits → toast résumé "N conflits détectés"
 *   - reset compteur et debounce quand tous les toasts sont fermés
 *
 * Pattern : mock de onEvent (wails service) pour contrôler les événements
 * reçus sans dépendre du runtime Wails.
 */

import { renderHook, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { useSyncStatus } from './useSyncStatus';
import type { ConflictEvent } from '../types/ghostdrive';

// ── Mock wails service ─────────────────────────────────────────────────────────

// Capture all event listeners registered via onEvent.
type EventHandler = (payload: unknown) => void;

const { mockOnEvent, mockGetSyncState } = vi.hoisted(() => ({
  mockOnEvent:      vi.fn(),
  mockGetSyncState: vi.fn(),
}));

vi.mock('../services/wails', () => ({
  onEvent: mockOnEvent,
  ghostdriveApi: {
    getSyncState: mockGetSyncState,
  },
}));

// ── Helpers ───────────────────────────────────────────────────────────────────

/** Registry of listeners installed by onEvent during a test. */
const listeners: Record<string, EventHandler[]> = {};

/** Emit a synthetic Wails event into the hook's registered listeners. */
function emitEvent(name: string, payload: unknown): void {
  (listeners[name] ?? []).forEach(fn => fn(payload));
}

/** Build a minimal ConflictEvent for the given path. */
function makeConflict(path: string): ConflictEvent {
  return {
    backendID:      'backend-1',
    path,
    localModTime:   '2026-05-23T10:00:00Z',
    remoteModTime:  '2026-05-23T10:05:00Z',
    resolution:     'local-wins',
  };
}

/** Default idle SyncState returned by getSyncState. */
const idleSyncState = {
  status:          'idle',
  progress:        0,
  currentFile:     '',
  pending:         0,
  errors:          [],
  lastSync:        '',
  backends:        [],
  activeTransfers: [],
};

// ── Setup / Teardown ──────────────────────────────────────────────────────────

beforeEach(() => {
  vi.useFakeTimers();

  // Clear listener registry.
  for (const key of Object.keys(listeners)) {
    delete listeners[key];
  }

  // mockOnEvent registers the callback in our registry and returns an unsubscribe fn.
  mockOnEvent.mockImplementation((event: string, cb: EventHandler) => {
    if (!listeners[event]) listeners[event] = [];
    listeners[event].push(cb);
    return () => {
      listeners[event] = (listeners[event] ?? []).filter(fn => fn !== cb);
    };
  });

  // getSyncState returns a resolved promise with idle state by default.
  mockGetSyncState.mockResolvedValue(idleSyncState);
});

afterEach(() => {
  vi.useRealTimers();
  vi.clearAllMocks();
});

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('useSyncStatus — sync:conflict toast', () => {

  it('affiche un toast individuel lors du premier conflit', async () => {
    const { result } = renderHook(() => useSyncStatus());

    // Let the initial getSyncState promise resolve.
    await act(async () => { await Promise.resolve(); });

    act(() => {
      emitEvent('sync:conflict', makeConflict('documents/rapport.docx'));
    });

    expect(result.current.conflictToasts).toHaveLength(1);
    expect(result.current.conflictToasts[0].type).toBe('conflict');
    expect(result.current.conflictToasts[0].message).toContain('rapport.docx');
    expect(result.current.conflictToasts[0].message).toContain('résolu automatiquement');
  });

  it('empile jusqu\'à 3 toasts individuels (paths distincts)', async () => {
    const { result } = renderHook(() => useSyncStatus());
    await act(async () => { await Promise.resolve(); });

    act(() => {
      emitEvent('sync:conflict', makeConflict('file-a.txt'));
      emitEvent('sync:conflict', makeConflict('file-b.txt'));
      emitEvent('sync:conflict', makeConflict('file-c.txt'));
    });

    expect(result.current.conflictToasts).toHaveLength(3);
    expect(result.current.conflictToasts.every(t => t.type === 'conflict')).toBe(true);
  });

  it('collapse en toast résumé quand > 3 conflits', async () => {
    const { result } = renderHook(() => useSyncStatus());
    await act(async () => { await Promise.resolve(); });

    act(() => {
      emitEvent('sync:conflict', makeConflict('file-a.txt'));
      emitEvent('sync:conflict', makeConflict('file-b.txt'));
      emitEvent('sync:conflict', makeConflict('file-c.txt'));
      emitEvent('sync:conflict', makeConflict('file-d.txt')); // 4th → collapse
    });

    // Exactly 1 summary toast.
    expect(result.current.conflictToasts).toHaveLength(1);
    const summary = result.current.conflictToasts[0];
    expect(summary.id).toBe('conflict-summary');
    expect(summary.message).toMatch(/4 conflits/);
    expect(summary.message).toContain('résolus automatiquement');
  });

  it('debounce 500 ms — deuxième événement même path ignoré dans la fenêtre', async () => {
    const { result } = renderHook(() => useSyncStatus());
    await act(async () => { await Promise.resolve(); });

    act(() => {
      emitEvent('sync:conflict', makeConflict('same-path.txt'));
    });
    expect(result.current.conflictToasts).toHaveLength(1);

    // Second event for the same path within < 500 ms → must be ignored.
    act(() => {
      vi.advanceTimersByTime(200); // still within debounce window
      emitEvent('sync:conflict', makeConflict('same-path.txt'));
    });

    expect(result.current.conflictToasts).toHaveLength(1);
  });

  it('debounce 500 ms — événement accepté après la fenêtre', async () => {
    const { result } = renderHook(() => useSyncStatus());
    await act(async () => { await Promise.resolve(); });

    act(() => {
      emitEvent('sync:conflict', makeConflict('debounce-path.txt'));
    });
    expect(result.current.conflictToasts).toHaveLength(1);

    // Advance past the debounce window.
    act(() => {
      vi.advanceTimersByTime(600); // > 500 ms
      emitEvent('sync:conflict', makeConflict('debounce-path.txt'));
    });

    expect(result.current.conflictToasts).toHaveLength(2);
  });

  it('dismissConflictToast — retire le toast cible et garde les autres', async () => {
    const { result } = renderHook(() => useSyncStatus());
    await act(async () => { await Promise.resolve(); });

    act(() => {
      emitEvent('sync:conflict', makeConflict('a.txt'));
      emitEvent('sync:conflict', makeConflict('b.txt'));
    });
    expect(result.current.conflictToasts).toHaveLength(2);

    const idToRemove = result.current.conflictToasts[0].id;

    act(() => {
      result.current.dismissConflictToast(idToRemove);
    });

    expect(result.current.conflictToasts).toHaveLength(1);
    expect(result.current.conflictToasts[0].id).not.toBe(idToRemove);
  });

  it('reset compteur et debounce quand tous les toasts sont fermés', async () => {
    const { result } = renderHook(() => useSyncStatus());
    await act(async () => { await Promise.resolve(); });

    // Add 1 toast.
    act(() => {
      emitEvent('sync:conflict', makeConflict('reset-path.txt'));
    });
    expect(result.current.conflictToasts).toHaveLength(1);
    const id = result.current.conflictToasts[0].id;

    // Dismiss it → batch ended → counters reset.
    act(() => {
      result.current.dismissConflictToast(id);
    });
    expect(result.current.conflictToasts).toHaveLength(0);

    // After reset: same path can trigger a new toast (debounce cleared).
    act(() => {
      emitEvent('sync:conflict', makeConflict('reset-path.txt'));
    });

    expect(result.current.conflictToasts).toHaveLength(1);
  });

  it('paths différents dans la même émission créent des toasts indépendants', async () => {
    const { result } = renderHook(() => useSyncStatus());
    await act(async () => { await Promise.resolve(); });

    act(() => {
      emitEvent('sync:conflict', makeConflict('alpha.txt'));
      emitEvent('sync:conflict', makeConflict('beta.txt'));
    });

    // Two toasts, each referencing a different path.
    expect(result.current.conflictToasts).toHaveLength(2);
    const messages = result.current.conflictToasts.map(t => t.message);
    expect(messages.some(m => m.includes('alpha.txt'))).toBe(true);
    expect(messages.some(m => m.includes('beta.txt'))).toBe(true);
  });

  it('counter continue d\'augmenter après le collapse (>3)', async () => {
    const { result } = renderHook(() => useSyncStatus());
    await act(async () => { await Promise.resolve(); });

    act(() => {
      emitEvent('sync:conflict', makeConflict('f1.txt'));
      emitEvent('sync:conflict', makeConflict('f2.txt'));
      emitEvent('sync:conflict', makeConflict('f3.txt'));
      emitEvent('sync:conflict', makeConflict('f4.txt')); // collapse: "4 conflits"
      emitEvent('sync:conflict', makeConflict('f5.txt')); // "5 conflits"
    });

    expect(result.current.conflictToasts).toHaveLength(1);
    expect(result.current.conflictToasts[0].message).toMatch(/5 conflits/);
  });

});
