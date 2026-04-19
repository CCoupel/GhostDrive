import { useEffect } from 'react';
import { CloudUpload, CheckCircle, Pause, AlertCircle, RefreshCw } from 'lucide-react';
import { onEvent } from '../../services/wails';
import type { SyncState, SyncStatus } from '../../types/ghostdrive';

interface TrayStatusProps {
  syncState: SyncState;
  onNavigateToSettings?: () => void;
}

const statusConfig: Record<SyncStatus, { label: string; icon: typeof CheckCircle; color: string }> = {
  idle:    { label: 'Synchronisé',        icon: CheckCircle, color: 'text-status-idle' },
  syncing: { label: 'Synchronisation...', icon: RefreshCw,   color: 'text-status-syncing' },
  paused:  { label: 'En pause',           icon: Pause,       color: 'text-status-paused' },
  error:   { label: 'Erreur',             icon: AlertCircle, color: 'text-status-error' },
};

export function TrayStatus({ syncState, onNavigateToSettings }: TrayStatusProps) {
  const { status, backends } = syncState;
  const { label, icon: Icon, color } = statusConfig[status];

  const totalPending = backends.reduce((sum, b) => sum + b.pending, 0);
  const currentFile = backends.find(b => b.status === 'syncing')?.currentFile ?? '';

  useEffect(() => {
    if (!onNavigateToSettings) return;
    return onEvent('tray:open-settings', () => onNavigateToSettings());
  }, [onNavigateToSettings]);

  return (
    <div className="flex items-center gap-2 px-3 py-2">
      <CloudUpload size={20} className="text-brand shrink-0" />
      <div className="flex-1 min-w-0">
        <div className={`flex items-center gap-1.5 font-medium ${color}`}>
          <Icon size={13} className={status === 'syncing' ? 'animate-spin' : ''} />
          <span>{label}</span>
          {status === 'syncing' && totalPending > 0 && (
            <span className="text-xs text-gray-500 font-normal">({totalPending} fichiers)</span>
          )}
        </div>
        {currentFile && status === 'syncing' && (
          <p className="text-xs text-gray-500 truncate mt-0.5">{currentFile}</p>
        )}
      </div>
    </div>
  );
}
