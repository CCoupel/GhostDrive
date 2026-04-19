import { useState, useEffect } from 'react';
import { ghostdriveApi, onEvent } from '../services/wails';
import type { SyncState, ProgressEvent, SyncError, FileEvent } from '../types/ghostdrive';

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

export function useSyncStatus() {
  const [syncState, setSyncState] = useState<SyncState>(INITIAL_STATE);
  const [activeTransfers, setActiveTransfers] = useState<Map<string, ProgressEvent>>(new Map());
  const [errors, setErrors] = useState<SyncError[]>([]);
  const [recentEvents, setRecentEvents] = useState<FileEvent[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let mounted = true;

    ghostdriveApi.getSyncState()
      .then(state => { if (mounted) { setSyncState(state); setLoading(false); } })
      .catch(() => { if (mounted) setLoading(false); });

    const unsubState = onEvent('sync:state-changed', (state) => {
      if (!mounted) return;
      setSyncState(state);
      if (state.status === 'idle') {
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

    return () => {
      mounted = false;
      unsubState();
      unsubProgress();
      unsubError();
      unsubFileEvent();
    };
  }, []);

  return {
    syncState,
    activeTransfers: Array.from(activeTransfers.values()),
    errors,
    recentEvents,
    loading,
  };
}
