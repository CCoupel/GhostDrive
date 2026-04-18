import { File, Folder, Download, Cloud, HardDrive } from 'lucide-react';
import type { FileInfo } from '../../types/ghostdrive';
import { Button } from '../ui/Button';

interface FileListProps {
  files: FileInfo[];
  loading?: boolean;
  onDownload?: (file: FileInfo) => void;
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} o`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} Ko`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} Mo`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} Go`;
}

function formatDate(iso: string): string {
  if (!iso) return '';
  return new Date(iso).toLocaleDateString('fr-FR', { day: '2-digit', month: 'short', year: 'numeric' });
}

export function FileList({ files, loading, onDownload }: FileListProps) {
  if (loading) {
    return (
      <div className="flex items-center justify-center h-24 text-gray-400 text-sm">
        Chargement...
      </div>
    );
  }

  if (files.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center h-24 text-gray-400 gap-1">
        <Folder size={28} className="opacity-30" />
        <span className="text-sm">Aucun fichier</span>
      </div>
    );
  }

  return (
    <ul className="divide-y divide-surface-border" role="list">
      {files.map(file => (
        <li key={file.path} className="flex items-center gap-2 px-3 py-2 hover:bg-surface-secondary group">
          <span className="shrink-0 text-gray-400">
            {file.isDir ? <Folder size={16} /> : <File size={16} />}
          </span>

          <div className="flex-1 min-w-0">
            <p className="text-sm truncate text-gray-900">{file.name}</p>
            <p className="text-xs text-gray-400">
              {!file.isDir && formatSize(file.size)}
              {!file.isDir && ' · '}
              {formatDate(file.modTime)}
            </p>
          </div>

          <div className="flex items-center gap-1 shrink-0">
            {file.isCached && (
              <span title="En cache local">
                <HardDrive size={12} className="text-status-idle" />
              </span>
            )}
            {file.isPlaceholder && (
              <span title="Placeholder — non téléchargé">
                <Cloud size={12} className="text-status-syncing" />
              </span>
            )}
            {!file.isDir && onDownload && file.isPlaceholder && (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => onDownload(file)}
                aria-label={`Télécharger ${file.name}`}
                className="opacity-0 group-hover:opacity-100 transition-opacity"
              >
                <Download size={12} />
              </Button>
            )}
          </div>
        </li>
      ))}
    </ul>
  );
}
