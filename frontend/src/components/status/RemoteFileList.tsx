import { useState, useEffect, useCallback } from 'react';
import { Folder, File, Download, ChevronRight, RefreshCw } from 'lucide-react';
import { ghostdriveApi, onEvent } from '../../services/wails';
import { ProgressBar } from './ProgressBar';
import { formatBytes } from '../../utils/formatBytes';
import { formatRelative } from '../../utils/formatRelative';
import type { FileInfo, ProgressEvent } from '../../types/ghostdrive';

interface RemoteFileListProps {
  backendId: string;
  path: string;
  onNavigate: (path: string) => void;
}

interface DownloadState {
  percent: number;
  bytesDone: number;
  bytesTotal: number;
}

/**
 * Lists remote files for a given backend + path.
 * - Click folder  → onNavigate(folder.path)
 * - Click file    → DownloadFile + inline progress bar
 * - sync:progress → updates progress per-file; clears isPlaceholder on completion
 */
export function RemoteFileList({ backendId, path, onNavigate }: RemoteFileListProps) {
  const [files, setFiles] = useState<FileInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  // key = remotePath, value = download progress state
  const [downloads, setDownloads] = useState<Map<string, DownloadState>>(new Map());

  // ── Load file list ────────────────────────────────────────────────────────
  useEffect(() => {
    let active = true;
    setLoading(true);
    setError(null);
    setFiles([]);
    // Clear stale download indicators when navigating to a different path
    setDownloads(new Map());

    ghostdriveApi.listFiles(backendId, path)
      .then(result => {
        if (!active) return;
        setFiles(result ?? []);
        setLoading(false);
      })
      .catch(err => {
        if (!active) return;
        setError(String(err));
        setLoading(false);
      });

    return () => { active = false; };
  }, [backendId, path]);

  // ── Download progress via sync:progress ──────────────────────────────────
  useEffect(() => {
    const unsub = onEvent('sync:progress', (evt: ProgressEvent) => {
      if (evt.direction !== 'download') return;
      // Filter to this backend when backendId is available in payload
      if (evt.backendId && evt.backendId !== backendId) return;

      const key = evt.remotePath ?? evt.path;

      setDownloads(prev => {
        const next = new Map(prev);
        if (evt.percent >= 100) {
          next.delete(key);
          // Mark as hydrated in the file list
          setFiles(prev => prev.map(f =>
            f.path === key ? { ...f, isPlaceholder: false } : f,
          ));
        } else {
          next.set(key, {
            percent: evt.percent,
            bytesDone: evt.bytesDone,
            bytesTotal: evt.bytesTotal,
          });
        }
        return next;
      });
    });

    return unsub;
  }, [backendId]);

  // ── Interactions ──────────────────────────────────────────────────────────
  const handleClick = useCallback(async (file: FileInfo) => {
    if (file.isDir) {
      onNavigate(file.path);
    } else {
      try {
        await ghostdriveApi.downloadFile(backendId, file.path);
      } catch {
        // Errors surface via sync:error events — no local handling needed
      }
    }
  }, [backendId, onNavigate]);

  // ── Render states ─────────────────────────────────────────────────────────
  if (loading) {
    return (
      <div className="flex items-center justify-center h-32 gap-2 text-gray-400 text-sm">
        <RefreshCw size={14} className="animate-spin" />
        <span>Chargement...</span>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex items-center justify-center h-32 p-4 text-sm text-red-500 text-center">
        Erreur : {error}
      </div>
    );
  }

  if (files.length === 0) {
    return (
      <div className="flex items-center justify-center h-32 text-sm text-gray-400">
        Ce dossier est vide
      </div>
    );
  }

  // ── File list ─────────────────────────────────────────────────────────────
  return (
    <div
      role="list"
      aria-label={`Contenu de ${path || 'la racine'}`}
      className="divide-y divide-surface-border"
    >
      {files.map(file => {
        const dlState = downloads.get(file.path);
        const isDownloading = Boolean(dlState);

        return (
          <div key={file.path} role="listitem">
            <button
              onClick={() => handleClick(file)}
              disabled={!file.isDir && isDownloading}
              className="group w-full flex items-center gap-3 px-4 py-2.5 text-left
                hover:bg-surface-secondary transition-colors focus-visible:outline-none
                focus-visible:ring-2 focus-visible:ring-brand focus-visible:ring-inset
                disabled:cursor-not-allowed disabled:opacity-75"
              aria-label={`${file.isDir ? 'Ouvrir le dossier' : 'Télécharger le fichier'} ${file.name}`}
              aria-busy={isDownloading || undefined}
            >
              {/* Icon */}
              <span
                className={`shrink-0 ${file.isDir ? 'text-brand' : 'text-gray-400'}`}
                aria-hidden="true"
              >
                {file.isDir ? <Folder size={16} /> : <File size={16} />}
              </span>

              {/* Name */}
              <span className="flex-1 min-w-0 text-sm text-gray-800 truncate">
                {file.name}
              </span>

              {/* Placeholder cloud indicator */}
              {!file.isDir && file.isPlaceholder && !isDownloading && (
                <span
                  className="shrink-0 text-xs text-gray-300"
                  title="Fichier distant non téléchargé"
                  aria-label="Fichier distant"
                >
                  ☁
                </span>
              )}

              {/* Download icon (visible on hover, hidden when downloading) */}
              {!file.isDir && !isDownloading && (
                <span
                  className="shrink-0 opacity-0 group-hover:opacity-100 transition-opacity text-gray-400"
                  aria-hidden="true"
                >
                  <Download size={13} />
                </span>
              )}

              {/* Spinning indicator when downloading */}
              {isDownloading && (
                <span className="shrink-0 text-brand" aria-label="Téléchargement en cours">
                  <RefreshCw size={13} className="animate-spin" />
                </span>
              )}

              {/* File size */}
              {!file.isDir && (
                <span className="shrink-0 w-16 text-right text-xs text-gray-400 tabular-nums">
                  {formatBytes(file.size)}
                </span>
              )}

              {/* Modified date */}
              <span className="hidden sm:block shrink-0 w-20 text-right text-xs text-gray-400">
                {formatRelative(file.modTime)}
              </span>

              {/* Folder navigate chevron */}
              {file.isDir && (
                <ChevronRight size={14} className="shrink-0 text-gray-300" aria-hidden="true" />
              )}
            </button>

            {/* Inline download progress bar */}
            {isDownloading && dlState && (
              <div className="px-4 pb-2.5" aria-live="polite" aria-atomic="true">
                <ProgressBar
                  value={dlState.percent}
                  label={`${formatBytes(dlState.bytesDone)} / ${formatBytes(dlState.bytesTotal)}`}
                  showPercent
                  className="text-xs"
                  aria-label={`Téléchargement de ${file.name} : ${Math.round(dlState.percent)}%`}
                />
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}
