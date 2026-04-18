import { AlertTriangle, CheckCircle, Clock, RefreshCw } from 'lucide-react';
import { ProgressBar } from './ProgressBar';
import type { SyncState, ProgressEvent } from '../../types/ghostdrive';

interface SyncStatusProps {
  syncState: SyncState;
  activeTransfers: ProgressEvent[];
}

function formatDate(iso: string): string {
  if (!iso) return 'Jamais';
  return new Date(iso).toLocaleString('fr-FR', {
    day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit',
  });
}

export function SyncStatusPanel({ syncState, activeTransfers }: SyncStatusProps) {
  const { status, progress, currentFile, pending, errors, lastSync } = syncState;

  return (
    <div className="flex flex-col gap-3 p-3">
      {status === 'syncing' && (
        <div className="flex flex-col gap-2">
          <div className="flex items-center gap-2 text-status-syncing text-sm font-medium">
            <RefreshCw size={14} className="animate-spin" />
            <span>Synchronisation en cours</span>
            {pending > 0 && <span className="text-gray-500 font-normal">({pending} restants)</span>}
          </div>
          <ProgressBar value={progress * 100} showPercent />
          {currentFile && (
            <p className="text-xs text-gray-500 truncate">{currentFile}</p>
          )}
        </div>
      )}

      {activeTransfers.length > 0 && (
        <div className="flex flex-col gap-1.5">
          <p className="text-xs font-medium text-gray-500 uppercase tracking-wide">Transferts actifs</p>
          {activeTransfers.slice(0, 5).map(t => (
            <ProgressBar
              key={t.path}
              value={t.percent}
              label={`${t.direction === 'upload' ? '↑' : '↓'} ${t.path.split('/').pop()}`}
              showPercent
              className="text-xs"
            />
          ))}
        </div>
      )}

      {status === 'idle' && (
        <div className="flex items-center gap-2 text-status-idle text-sm">
          <CheckCircle size={14} />
          <span>Tout est synchronisé</span>
        </div>
      )}

      {errors.length > 0 && (
        <div className="flex flex-col gap-1.5">
          <p className="text-xs font-medium text-red-500 uppercase tracking-wide flex items-center gap-1">
            <AlertTriangle size={11} />
            Erreurs ({errors.length})
          </p>
          <ul className="space-y-1 max-h-28 overflow-y-auto">
            {errors.map((err, i) => (
              <li key={i} className="text-xs text-red-600 bg-red-50 rounded px-2 py-1 truncate" title={err.message}>
                {err.path}: {err.message}
              </li>
            ))}
          </ul>
        </div>
      )}

      <div className="flex items-center gap-1.5 text-xs text-gray-400 mt-auto">
        <Clock size={11} />
        <span>Dernière sync : {formatDate(lastSync)}</span>
      </div>
    </div>
  );
}
