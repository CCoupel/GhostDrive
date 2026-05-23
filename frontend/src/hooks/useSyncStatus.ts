import { useState, useEffect, useCallback, useRef } from 'react';
import { ghostdriveApi, onEvent } from '../services/wails';
import type { SyncState, ProgressEvent, SyncError, FileEvent } from '../types/ghostdrive';

// ── Toast Conflict (v2.2 #144) ────────────────────────────────────────────────

/** Item de toast affiché à l'utilisateur lors d'un conflit auto-résolu. */
export interface ConflictToast {
  id:      string;
  message: string;
  type:    'conflict';
}

const MAX_CONFLICT_TOASTS  = 3;
const CONFLICT_DEBOUNCE_MS = 500;

const INITIAL_STATE: SyncState = {
  status: 'idle',
  progress: 0,
  currentFile: '',
  pending: 0,
  errors: [],
  lastSync: '',
  backends: [],
  activeTransfers: [],
};

const MAX_ERRORS = 50;
const MAX_EVENTS = 100;

/** Normalize a raw SyncState from Go — nil slices serialize as JSON null. */
function normalizeSyncState(state: SyncState): SyncState {
  return {
    ...state,
    errors:          state.errors          ?? [],
    backends:        state.backends        ?? [],
    activeTransfers: state.activeTransfers ?? [],
  };
}

export function useSyncStatus() {
  const [syncState, setSyncState] = useState<SyncState>(INITIAL_STATE);
  const [activeTransfers, setActiveTransfers] = useState<Map<string, ProgressEvent>>(new Map());
  const [errors, setErrors] = useState<SyncError[]>([]);
  const [recentEvents, setRecentEvents] = useState<FileEvent[]>([]);
  const [loading, setLoading] = useState(true);

  // ── Conflict toasts (v2.2 #144) ─────────────────────────────────────────────
  const [conflictToasts, setConflictToasts] = useState<ConflictToast[]>([]);

  /**
   * Compteur cumulatif de conflits dans le "batch" courant.
   * Un batch se termine quand l'utilisateur a fermé tous les toasts (length → 0).
   * Stocker dans un ref évite de recréer le listener sync:conflict à chaque rendu.
   */
  const conflictCountRef  = useRef(0);
  /** Debounce par chemin : timestamp du dernier toast affiché pour ce path. */
  const lastToastTimeRef  = useRef(new Map<string, number>());

  const dismissConflictToast = useCallback((id: string) => {
    setConflictToasts(prev => {
      const next = prev.filter(t => t.id !== id);
      if (next.length === 0) {
        // Batch terminé — réinitialiser le compteur et le debounce
        conflictCountRef.current = 0;
        lastToastTimeRef.current.clear();
      }
      return next;
    });
  }, []);

  useEffect(() => {
    let mounted = true;

    ghostdriveApi.getSyncState()
      .then(state => {
        if (mounted) {
          setSyncState(normalizeSyncState(state));
          setLoading(false);
        }
      })
      .catch(() => { if (mounted) setLoading(false); });

    const unsubState = onEvent('sync:state-changed', (state) => {
      if (!mounted) return;
      const normalized = normalizeSyncState(state);
      setSyncState(normalized);
      if (normalized.status === 'idle') {
        setActiveTransfers(new Map());
      }
    });

    const unsubProgress = onEvent('sync:progress', (evt) => {
      if (!mounted) return;
      setActiveTransfers(prev => {
        const next = new Map(prev);
        if (evt.percent >= 100) {
          next.delete(evt.path);
        } else {
          next.set(evt.path, evt);
        }
        return next;
      });
    });

    const unsubError = onEvent('sync:error', (err) => {
      if (!mounted) return;
      setErrors(prev => {
        const next = [...prev, err];
        return next.length > MAX_ERRORS ? next.slice(next.length - MAX_ERRORS) : next;
      });
    });

    const unsubFileEvent = onEvent('sync:file-event', (evt) => {
      if (!mounted) return;
      setRecentEvents(prev => {
        const next = [evt, ...prev];
        return next.length > MAX_EVENTS ? next.slice(0, MAX_EVENTS) : next;
      });
    });

    // ── sync:conflict — toast non-bloquant (v2.2 #144) ──────────────────────
    const unsubConflict = onEvent('sync:conflict', (evt) => {
      if (!mounted) return;

      // Debounce : ignorer si le même path a déjà déclenché un toast < 500 ms
      const now = Date.now();
      const lastTime = lastToastTimeRef.current.get(evt.path) ?? 0;
      if (now - lastTime < CONFLICT_DEBOUNCE_MS) return;
      lastToastTimeRef.current.set(evt.path, now);

      conflictCountRef.current += 1;
      const count = conflictCountRef.current;

      setConflictToasts(prev => {
        if (count > MAX_CONFLICT_TOASTS) {
          // Regrouper en un seul toast résumé
          return [{
            id:      'conflict-summary',
            message: `${count} conflits détectés (résolus automatiquement)`,
            type:    'conflict',
          }];
        }
        // Toast individuel
        const newToast: ConflictToast = {
          id:      `conflict-${now}-${Math.random().toString(36).slice(2, 7)}`,
          message: `Conflit détecté : ${evt.path} (résolu automatiquement)`,
          type:    'conflict',
        };
        return [...prev, newToast];
      });
    });

    return () => {
      mounted = false;
      unsubState();
      unsubProgress();
      unsubError();
      unsubFileEvent();
      unsubConflict();
    };
  }, []);

  return {
    syncState,
    activeTransfers: Array.from(activeTransfers.values()),
    errors,
    recentEvents,
    loading,
    conflictToasts,
    dismissConflictToast,
  };
}
