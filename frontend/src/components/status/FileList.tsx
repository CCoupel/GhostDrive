import { FilePlus, FilePenLine, Trash2, ArrowLeftRight, HardDrive, Cloud } from 'lucide-react';
import type { FileEvent, FileEventType } from '../../types/ghostdrive';
import { formatRelative } from '../../utils/formatRelative';

interface FileListProps {
  events: FileEvent[];
  className?: string;
}

const eventConfig: Record<FileEventType, { icon: typeof FilePlus; color: string; label: string }> = {
  created:  { icon: FilePlus,      color: 'text-status-idle',    label: 'Créé' },
  modified: { icon: FilePenLine,   color: 'text-status-syncing', label: 'Modifié' },
  deleted:  { icon: Trash2,        color: 'text-status-error',   label: 'Supprimé' },
  renamed:  { icon: ArrowLeftRight, color: 'text-status-paused', label: 'Renommé' },
};


export function FileList({ events, className = '' }: FileListProps) {
  if (events.length === 0) {
    return (
      <div className={`flex items-center justify-center py-6 text-gray-400 text-sm ${className}`}>
        Aucun fichier synchronisé récemment.
      </div>
    );
  }

  return (
    <ul className={`divide-y divide-surface-border ${className}`} role="list">
      {events.map((evt, i) => {
        const { icon: Icon, color, label } = eventConfig[evt.type];
        return (
          <li
            key={`${evt.path}-${evt.timestamp}-${i}`}
            className="flex items-center gap-2 px-3 py-2 hover:bg-surface-secondary"
          >
            <span className={`shrink-0 ${color}`} title={label}>
              <Icon size={14} />
            </span>

            <div className="flex-1 min-w-0">
              <p className="text-xs font-medium text-gray-800 truncate" title={evt.path}>
                {evt.path.split(/[/\\]/).pop() ?? evt.path}
              </p>
              <p className="text-xs text-gray-400 truncate">{evt.path}</p>
            </div>

            <div className="flex items-center gap-1.5 shrink-0">
              <span title={evt.source === 'local' ? 'Source locale' : 'Source distante'}>
                {evt.source === 'local'
                  ? <HardDrive size={11} className="text-gray-400" />
                  : <Cloud size={11} className="text-gray-400" />
                }
              </span>
              <span className="text-xs text-gray-400 tabular-nums">
                {formatRelative(evt.timestamp)}
              </span>
            </div>
          </li>
        );
      })}
    </ul>
  );
}
