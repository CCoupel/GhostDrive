import { useState } from 'react';
import {
  AlertTriangle, CheckCircle, Clock, RefreshCw, Pause, Play, Square, ChevronDown, ChevronUp,
} from 'lucide-react';
import { formatRelative } from '../../utils/formatRelative';
import { ProgressBar, TransferProgressBar } from './ProgressBar';
import { FileList } from './FileList';
import { Button } from '../ui/Button';
import { ghostdriveApi } from '../../services/wails';
import type { SyncState, ProgressEvent, SyncError, FileEvent, BackendSyncState } from '../../types/ghostdrive';

interface SyncStatusProps {
  syncState: SyncState;
  activeTransfers: ProgressEvent[];
  errors: SyncError[];
  recentEvents: FileEvent[];
}


export function SyncStatusPanel({ syncState, activeTransfers, errors, recentEvents }: SyncStatusProps) {
  const [errorsExpanded, setErrorsExpanded] = useState(false);
  const [eventsExpanded, setEventsExpanded] = useState(false);

  const { status, progress, backends } = syncState;
  const visibleErrors = errorsExpanded ? errors : errors.slice(0, 3);

  return (
    <div className="flex flex-col gap-3 p-3">
      {/* Global status header */}
      <div className="flex items-center gap-2">
        {status === 'syncing' && (
          <div className="flex-1 flex flex-col gap-1.5">
            <div className="flex items-center gap-2 text-status-syncing text-sm font-medium">
              <RefreshCw size={14} className="animate-spin" />
              <span>Synchronisation en cours</span>
            </div>
            <ProgressBar value={progress * 100} showPercent />
          </div>
        )}
        {status === 'idle' && (
          <div className="flex items-center gap-2 text-status-idle text-sm">
            <CheckCircle size={14} />
            <span>Tout est synchronisé</span>
          </div>
        )}
        {status === 'paused' && (
          <div className="flex items-center gap-2 text-status-paused text-sm font-medium">
            <Pause size={14} />
            <span>Synchronisation en pause</span>
          </div>
        )}
        {status === 'error' && (
          <div className="flex items-center gap-2 text-status-error text-sm font-medium">
            <AlertTriangle size={14} />
            <span>Erreur de synchronisation</span>
          </div>
        )}
      </div>

      {/* Active transfers */}
      {activeTransfers.length > 0 && (
        <div className="flex flex-col gap-1.5">
          <p className="text-xs font-medium text-gray-500 uppercase tracking-wide">
            Transferts actifs ({activeTransfers.length})
          </p>
          {activeTransfers.slice(0, 5).map(t => (
            <TransferProgressBar key={t.path} transfer={t} className="text-xs" />
          ))}
          {activeTransfers.length > 5 && (
            <p className="text-xs text-gray-400">
              +{activeTransfers.length - 5} autres transferts...
            </p>
          )}
        </div>
      )}

      {/* Per-backend statuses */}
      {backends.length > 0 && (
        <div className="flex flex-col gap-2">
          <p className="text-xs font-medium text-gray-500 uppercase tracking-wide">Backends</p>
          {backends.map(b => (
            <BackendRow key={b.backendId} backend={b} />
          ))}
        </div>
      )}

      {/* Errors */}
      {errors.length > 0 && (
        <div className="flex flex-col gap-1.5">
          <button
            onClick={() => setErrorsExpanded(v => !v)}
            className="flex items-center gap-1 text-xs font-medium text-red-500 uppercase tracking-wide hover:text-red-600"
          >
            <AlertTriangle size={11} />
            Erreurs ({errors.length})
            {errors.length > 3 && (errorsExpanded ? <ChevronUp size={11} /> : <ChevronDown size={11} />)}
          </button>
          <ul className="space-y-1">
            {visibleErrors.map((err, i) => (
              <li
                key={i}
                className="text-xs text-red-600 bg-red-50 rounded px-2 py-1 truncate"
                title={err.message}
              >
                {err.path}: {err.message}
              </li>
            ))}
          </ul>
          {!errorsExpanded && errors.length > 3 && (
            <button
              onClick={() => setErrorsExpanded(true)}
              className="text-xs text-red-400 hover:text-red-500 text-left"
            >
              Voir {errors.length - 3} erreur{errors.length - 3 > 1 ? 's' : ''} de plus
            </button>
          )}
        </div>
      )}

      {/* Recent events */}
      {recentEvents.length > 0 && (
        <div className="flex flex-col gap-1">
          <button
            onClick={() => setEventsExpanded(v => !v)}
            className="flex items-center gap-1 text-xs font-medium text-gray-500 uppercase tracking-wide hover:text-gray-700"
          >
            <Clock size={11} />
            Fichiers récents ({recentEvents.length})
            {eventsExpanded ? <ChevronUp size={11} /> : <ChevronDown size={11} />}
          </button>
          {eventsExpanded && (
            <FileList events={recentEvents.slice(0, 20)} className="rounded border border-surface-border mt-1" />
          )}
        </div>
      )}
    </div>
  );
}

function BackendRow({ backend }: { backend: BackendSyncState }) {
  const [busy, setBusy] = useState(false);
  const { backendId, backendName, status, progress, currentFile, pending, lastSync } = backend;

  const isSyncing = status === 'syncing';
  const isPaused  = status === 'paused';

  const handleToggle = async () => {
    setBusy(true);
    try {
      if (isSyncing) await ghostdriveApi.pauseSync(backendId);
      else await ghostdriveApi.startSync(backendId);
    } finally {
      setBusy(false);
    }
  };

  const handleStop = async () => {
    setBusy(true);
    try { await ghostdriveApi.stopSync(backendId); }
    finally { setBusy(false); }
  };

  const handleForce = async () => {
    setBusy(true);
    try { await ghostdriveApi.forceSync(backendId); }
    finally { setBusy(false); }
  };

  return (
    <div className="rounded border border-surface-border p-2 bg-white">
      <div className="flex items-center justify-between gap-2 mb-1">
        <div className="flex items-center gap-1.5 min-w-0">
          <span className={`status-dot
            ${status === 'idle'    ? 'status-dot-idle'    : ''}
            ${status === 'syncing' ? 'status-dot-syncing' : ''}
            ${status === 'paused'  ? 'status-dot-paused'  : ''}
            ${status === 'error'   ? 'status-dot-error'   : ''}
          `} />
          <span className="text-sm font-medium text-gray-800 truncate">{backendName}</span>
          {pending > 0 && (
            <span className="text-xs text-gray-400">({pending} en attente)</span>
          )}
        </div>
        <span className="text-xs text-gray-400 shrink-0">{formatRelative(lastSync)}</span>
      </div>

      {isSyncing && (
        <>
          <ProgressBar value={progress * 100} showPercent className="mb-1" />
          {currentFile && (
            <p className="text-xs text-gray-400 truncate mb-1">{currentFile}</p>
          )}
        </>
      )}

      <div className="flex gap-1 mt-1.5">
        <Button size="sm" variant="ghost" onClick={handleToggle} disabled={busy}>
          {isSyncing ? <Pause size={11} /> : <Play size={11} />}
          {isSyncing ? 'Pause' : isPaused ? 'Reprendre' : 'Sync'}
        </Button>
        {(isSyncing || isPaused) && (
          <Button size="sm" variant="ghost" onClick={handleStop} disabled={busy}>
            <Square size={11} /> Stop
          </Button>
        )}
        <Button size="sm" variant="ghost" onClick={handleForce} disabled={busy}>
          <RefreshCw size={11} className={busy && isSyncing ? 'animate-spin' : ''} /> Force
        </Button>
      </div>
    </div>
  );
}
