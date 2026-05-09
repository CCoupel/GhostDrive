import { useState, useEffect, useRef, useCallback } from 'react';
import { ghostdriveApi, onEvent } from '../services/wails';
import type { LogEntry, LogLevel } from '../types/ghostdrive';

const LEVELS: LogLevel[] = ['DEBUG', 'INFO', 'WARN', 'ERROR'];

const LEVEL_TEXT: Record<LogLevel, string> = {
  DEBUG: 'text-gray-400',
  INFO:  'text-sky-400',
  WARN:  'text-amber-400',
  ERROR: 'text-red-400',
};

const LEVEL_ROW_BG: Record<LogLevel, string> = {
  DEBUG: '',
  INFO:  '',
  WARN:  'bg-amber-950/20',
  ERROR: 'bg-red-950/40',
};

const LEVEL_CHIP_ACTIVE: Record<LogLevel, string> = {
  DEBUG: 'bg-gray-500 text-white',
  INFO:  'bg-sky-500 text-white',
  WARN:  'bg-amber-500 text-white',
  ERROR: 'bg-red-500 text-white',
};

const POLL_INTERVAL_MS = 2000;
const MAX_ENTRIES = 2000;

export function LogsPage() {
  const [entries, setEntries]       = useState<LogEntry[]>([]);
  const [paused, setPaused]         = useState(false);
  const [filter, setFilter]         = useState<LogLevel | null>(null);
  const [pendingCount, setPendingCount] = useState(0);

  const bottomRef  = useRef<HTMLDivElement>(null);
  const pausedRef  = useRef(false);
  const pendingRef = useRef<LogEntry[]>([]);
  // Tracks the highest log ID seen so far; polling fetches only entries after it.
  const lastIdRef  = useRef<number>(0);

  // Keep pausedRef in sync with state so event handlers see current value.
  useEffect(() => { pausedRef.current = paused; }, [paused]);

  // Polling every 2 s — fetches only new entries via sinceID.
  // Robust against Wails bridge not being ready at mount time (silently retries).
  // Runs once; uses lastIdRef (ref) to avoid re-subscribing on every new batch.
  useEffect(() => {
    let active = true;

    const poll = async () => {
      if (!active) return;
      try {
        const fresh = await ghostdriveApi.getLogs(lastIdRef.current);
        if (!active || !fresh || fresh.length === 0) return;

        lastIdRef.current = fresh[fresh.length - 1].id;

        if (pausedRef.current) {
          pendingRef.current = [...pendingRef.current, ...fresh];
          setPendingCount(c => c + fresh.length);
          return;
        }
        setEntries(prev => {
          const next = [...prev, ...fresh];
          return next.length > MAX_ENTRIES ? next.slice(-MAX_ENTRIES) : next;
        });
      } catch {
        // Bridge not ready — will retry on next interval tick
      }
    };

    poll(); // immediate call so logs appear as soon as the bridge is ready
    const id = setInterval(poll, POLL_INTERVAL_MS);
    return () => { active = false; clearInterval(id); };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // logs:new event — secondary safety net that catches entries emitted between
  // polling ticks. Deduplicates against lastIdRef to avoid double-counting.
  useEffect(() => {
    return onEvent('logs:new', (entry) => {
      if (entry.id <= lastIdRef.current) return; // already seen via polling
      lastIdRef.current = entry.id;

      if (pausedRef.current) {
        pendingRef.current = [...pendingRef.current, entry];
        setPendingCount(c => c + 1);
        return;
      }
      setEntries(prev => {
        const next = [...prev, entry];
        return next.length > MAX_ENTRIES ? next.slice(-MAX_ENTRIES) : next;
      });
    });
  }, []);

  // Auto-scroll to bottom when new entries arrive (only when not paused).
  useEffect(() => {
    if (!paused) {
      bottomRef.current?.scrollIntoView({ behavior: 'auto' });
    }
  }, [entries, paused]);

  const resume = useCallback(() => {
    setPaused(false);
    pausedRef.current = false;
    if (pendingRef.current.length > 0) {
      setEntries(prev => {
        const next = [...prev, ...pendingRef.current];
        pendingRef.current = [];
        setPendingCount(0);
        return next.length > MAX_ENTRIES ? next.slice(-MAX_ENTRIES) : next;
      });
    }
  }, []);

  // Refresh: full reload from ID 0 — clears display and refetches all stored entries.
  const handleRefresh = useCallback(() => {
    lastIdRef.current = 0;
    setEntries([]);
    pendingRef.current = [];
    setPendingCount(0);
    ghostdriveApi.getLogs(0)
      .then(data => {
        const all = data ?? [];
        if (all.length > 0) lastIdRef.current = all[all.length - 1].id;
        setEntries(all.length > MAX_ENTRIES ? all.slice(-MAX_ENTRIES) : all);
      })
      .catch(() => {});
  }, []);

  const handleClear = useCallback(() => {
    ghostdriveApi.clearLogs().catch(() => {});
    setEntries([]);
    pendingRef.current = [];
    setPendingCount(0);
    lastIdRef.current = 0;
  }, []);

  const toggleFilter = useCallback((lvl: LogLevel) => {
    setFilter(prev => prev === lvl ? null : lvl);
  }, []);

  const visible = filter ? entries.filter(e => e.level === filter) : entries;

  return (
    <div className="flex flex-col h-full bg-gray-950">
      {/* ── Toolbar ── */}
      <div className="flex items-center gap-2 px-3 py-2 border-b border-gray-800 bg-gray-900 shrink-0 flex-wrap">
        <span className="text-xs text-gray-400 font-medium">Niveau :</span>
        {LEVELS.map(lvl => (
          <button
            key={lvl}
            onClick={() => toggleFilter(lvl)}
            className={`px-2 py-0.5 rounded text-xs font-mono font-semibold transition-colors
              ${filter === lvl
                ? LEVEL_CHIP_ACTIVE[lvl]
                : 'bg-gray-800 text-gray-400 hover:bg-gray-700'}`}
          >
            {lvl}
          </button>
        ))}

        <div className="flex-1" />

        {/* entry count — local until GetLogCount() binding is available */}
        <span className="text-xs text-gray-500 font-mono tabular-nums">
          Affichage&nbsp;: {entries.length} entrée{entries.length !== 1 ? 's' : ''}
        </span>

        <button
          onClick={handleRefresh}
          title="Recharger tous les logs depuis le backend"
          className="px-3 py-1 rounded text-xs bg-gray-800 text-gray-300 hover:bg-gray-700 font-medium"
        >
          ↻ Refresh
        </button>

        {paused ? (
          <button
            onClick={resume}
            className="flex items-center gap-1.5 px-3 py-1 rounded text-xs bg-green-900 text-green-300 hover:bg-green-800 font-medium"
          >
            ▶ Reprendre{pendingCount > 0 ? ` (+${pendingCount})` : ''}
          </button>
        ) : (
          <button
            onClick={() => setPaused(true)}
            className="flex items-center gap-1.5 px-3 py-1 rounded text-xs bg-gray-800 text-gray-300 hover:bg-gray-700 font-medium"
          >
            ⏸ Pause
          </button>
        )}

        <button
          onClick={handleClear}
          className="px-3 py-1 rounded text-xs bg-gray-800 text-gray-400 hover:bg-red-900 hover:text-red-300 font-medium"
        >
          Effacer
        </button>
      </div>

      {/* ── Log list ── */}
      <div className="flex-1 overflow-y-auto font-mono text-xs text-gray-300 select-text">
        {visible.length === 0 ? (
          <div className="flex items-center justify-center h-32 text-gray-600">
            {filter ? `Aucun log de niveau ${filter}.` : 'Aucun log capturé.'}
          </div>
        ) : (
          visible.map(e => (
            <div
              key={e.id}
              className={`flex gap-2 px-3 py-px border-b border-gray-800/60 hover:bg-gray-800/40 ${LEVEL_ROW_BG[e.level as LogLevel]}`}
            >
              <span className="text-gray-600 shrink-0 tabular-nums">
                {e.time.length >= 19 ? e.time.slice(11, 19) : e.time}
              </span>
              <span className={`w-11 shrink-0 font-semibold ${LEVEL_TEXT[e.level as LogLevel]}`}>
                {e.level}
              </span>
              {e.source && (
                <span className="text-purple-400 shrink-0">[{e.source}]</span>
              )}
              <span className="break-all whitespace-pre-wrap">{e.message}</span>
            </div>
          ))
        )}
        <div ref={bottomRef} />
      </div>

      {/* ── Pause banner ── */}
      {paused && (
        <div className="shrink-0 flex items-center justify-between px-4 py-1.5 bg-amber-900/60 border-t border-amber-700/50 text-xs text-amber-300">
          <span>⏸ Logs en pause — {pendingCount} nouveaux message{pendingCount > 1 ? 's' : ''} en attente</span>
          <button onClick={resume} className="underline hover:text-amber-100">
            Reprendre
          </button>
        </div>
      )}
    </div>
  );
}
