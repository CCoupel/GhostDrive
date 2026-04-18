import { CloudUpload, CheckCircle, Pause, AlertCircle, RefreshCw } from 'lucide-react';
import type { SyncStatus } from '../../types/ghostdrive';

interface TrayStatusProps {
  status: SyncStatus;
  pending?: number;
  currentFile?: string;
}

const statusConfig: Record<SyncStatus, { label: string; icon: typeof CheckCircle; color: string }> = {
  idle:    { label: 'Synchronisé',         icon: CheckCircle,  color: 'text-status-idle' },
  syncing: { label: 'Synchronisation...',  icon: RefreshCw,    color: 'text-status-syncing' },
  paused:  { label: 'En pause',            icon: Pause,        color: 'text-status-paused' },
  error:   { label: 'Erreur',              icon: AlertCircle,  color: 'text-status-error' },
};

export function TrayStatus({ status, pending, currentFile }: TrayStatusProps) {
  const { label, icon: Icon, color } = statusConfig[status];

  return (
    <div className="flex items-center gap-2 px-3 py-2">
      <CloudUpload size={20} className="text-brand shrink-0" />
      <div className="flex-1 min-w-0">
        <div className={`flex items-center gap-1.5 font-medium ${color}`}>
          <Icon size={13} className={status === 'syncing' ? 'animate-spin' : ''} />
          <span>{label}</span>
          {status === 'syncing' && pending != null && pending > 0 && (
            <span className="text-xs text-gray-500 font-normal">({pending} fichiers)</span>
          )}
        </div>
        {currentFile && status === 'syncing' && (
          <p className="text-xs text-gray-500 truncate mt-0.5">{currentFile}</p>
        )}
      </div>
    </div>
  );
}
