import { useState, useEffect } from 'react';
import { ghostdriveApi, onEvent } from '../services/wails';
import type { SyncState, ProgressEvent } from '../types/ghostdrive';

const INITIAL_STATE: SyncState = {
  status: 'idle',
  progress: 0,
  currentFile: '',
  pending: 0,
  errors: [],
  lastSync: '',
};

export function useSyncStatus() {
  const [syncState, setSyncState] = useState<SyncState>(INITIAL_STATE);
  const [activeTransfers, setActiveTransfers] = useState<Map<string, ProgressEvent>>(new Map());
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let mounted = true;

    ghostdriveApi.getSyncState()
      .then(state => { if (mounted) { setSyncState(state); setLoading(false); } })
      .catch(() => { if (mounted) setLoading(false); });

    const unsubState = onEvent('sync:state-changed', (state) => {
      if (mounted) setSyncState(state);
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
      setSyncState(prev => ({
        ...prev,
        errors: [...prev.errors.slice(-9), err],
      }));
    });

    return () => {
      mounted = false;
      unsubState();
      unsubProgress();
      unsubError();
    };
  }, []);

  return { syncState, activeTransfers: Array.from(activeTransfers.values()), loading };
}
