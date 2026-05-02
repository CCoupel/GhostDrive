import { useState } from 'react';
import { HardDrive, Home, ChevronRight } from 'lucide-react';
import { RemoteFileList } from '../components/status/RemoteFileList';
import { useDriveStatuses } from '../hooks/useDriveStatus';
import { useBackends } from '../hooks/useBackends';

/**
 * FileBrowserPage — onglet "Drive"
 *
 * Affiche les fichiers distants de chaque backend connecté.
 * - Sélecteur de backend (onglets par nom) avec badge drive par backend
 * - Breadcrumb de navigation en profondeur
 *
 * v1.1.x : les drives sont montés/démontés via le toggle "Activer" dans l'onglet Backends.
 * Les boutons MonterGhD:/Démonter ont été supprimés (#88).
 */
export function FileBrowserPage() {
  const [selectedBackendId, setSelectedBackendId] = useState<string | null>(null);
  // pathStack[0] est toujours '' (racine), les éléments suivants sont des chemins empilés
  const [pathStack, setPathStack] = useState<string[]>(['']);

  const { configs, statuses, loading: backendsLoading } = useBackends();
  const { driveStatuses } = useDriveStatuses();

  // Backends disponibles pour la navigation : activés et connectés
  const connectedBackends = configs.filter(c => {
    if (!c.enabled) return false;
    const st = statuses.find(s => s.backendId === c.id);
    return st?.connected ?? false;
  });

  const currentPath = pathStack[pathStack.length - 1] ?? '';
  const selectedConfig = connectedBackends.find(c => c.id === selectedBackendId);

  const handleSelectBackend = (id: string) => {
    setSelectedBackendId(id);
    setPathStack(['']);
  };

  const handleNavigate = (newPath: string) => {
    setPathStack(prev => [...prev, newPath]);
  };

  const handleBreadcrumbClick = (stackIndex: number) => {
    setPathStack(prev => prev.slice(0, stackIndex + 1));
  };

  // ── Breadcrumb segments ───────────────────────────────────────────────────
  // pathStack = ['', '/photos', '/photos/2024']
  // On affiche : [BackendName] > photos > 2024
  const breadcrumbSegments = pathStack.slice(1).map((p, i) => ({
    label: p.split(/[/\\]/).filter(Boolean).pop() ?? p,
    fullPath: p,
    stackIndex: i + 1,
  }));

  // ── Render ────────────────────────────────────────────────────────────────
  return (
    <div className="flex flex-col h-full overflow-hidden">

      {/* ── Header ── */}
      <div className="shrink-0 flex items-center gap-2 px-4 py-3 bg-white border-b border-surface-border">
        <HardDrive size={16} className="text-brand" aria-hidden="true" />
        <span className="text-sm font-semibold text-gray-800">Fichiers distants</span>
        <span className="text-xs text-gray-400">
          — activez un backend pour monter son drive virtuel
        </span>
      </div>

      {/* ── No backends connected ── */}
      {!backendsLoading && connectedBackends.length === 0 && (
        <div className="flex-1 flex flex-col items-center justify-center gap-2 p-8 text-center text-gray-400">
          <HardDrive size={32} className="text-gray-200" aria-hidden="true" />
          <p className="text-sm">Aucun backend connecté.</p>
          <p className="text-xs">
            Ajoutez et activez un backend dans l&apos;onglet <strong>Backends</strong>.
          </p>
        </div>
      )}

      {connectedBackends.length > 0 && (
        <div className="flex flex-col flex-1 overflow-hidden">

          {/* ── Backend selector tabs with per-backend drive badge ── */}
          <div
            className="shrink-0 flex gap-0.5 px-3 pt-2 bg-white border-b border-surface-border overflow-x-auto"
            role="tablist"
            aria-label="Sélection du backend"
          >
            {connectedBackends.map(bc => {
              const ds = driveStatuses[bc.id];
              const isMounted = ds?.mounted ?? false;
              const mountLabel = bc.mountPoint || ds?.mountPoint || '';
              return (
                <button
                  key={bc.id}
                  role="tab"
                  aria-selected={selectedBackendId === bc.id}
                  onClick={() => handleSelectBackend(bc.id)}
                  className={`flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-t border-b-2 -mb-px
                    whitespace-nowrap transition-colors
                    ${selectedBackendId === bc.id
                      ? 'border-brand text-brand bg-surface-secondary'
                      : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300'
                    }`}
                >
                  {bc.name}
                  {mountLabel && (
                    <span
                      className={`inline-flex items-center gap-0.5 text-xs px-1 py-0.5 rounded font-mono
                        ${isMounted ? 'bg-green-100 text-green-700' : 'bg-gray-100 text-gray-400'}`}
                      title={isMounted ? `Drive ${mountLabel} monté` : `Drive ${mountLabel} non monté`}
                    >
                      <HardDrive size={9} aria-hidden="true" />
                      {mountLabel}
                    </span>
                  )}
                </button>
              );
            })}
          </div>

          {/* ── No backend selected ── */}
          {!selectedBackendId && (
            <div className="flex-1 flex items-center justify-center text-sm text-gray-400">
              Sélectionnez un backend ci-dessus
            </div>
          )}

          {/* ── Browser : breadcrumb + file list ── */}
          {selectedBackendId && (
            <div
              className="flex flex-col flex-1 overflow-hidden"
              role="tabpanel"
              aria-label={selectedConfig?.name ?? selectedBackendId}
            >
              {/* Breadcrumb */}
              <nav
                aria-label="Chemin de navigation"
                className="shrink-0 flex items-center gap-1 px-4 py-2 text-xs
                  text-gray-500 bg-surface-secondary border-b border-surface-border overflow-x-auto"
              >
                {/* Root */}
                <button
                  onClick={() => setPathStack([''])}
                  className="flex items-center gap-1 hover:text-brand transition-colors shrink-0"
                  aria-current={pathStack.length === 1 ? 'page' : undefined}
                >
                  <Home size={11} aria-hidden="true" />
                  <span>{selectedConfig?.name ?? selectedBackendId}</span>
                </button>

                {/* Sub-path segments */}
                {breadcrumbSegments.map(seg => (
                  <span key={seg.stackIndex} className="flex items-center gap-1 shrink-0">
                    <ChevronRight size={11} className="text-gray-300" aria-hidden="true" />
                    <button
                      onClick={() => handleBreadcrumbClick(seg.stackIndex)}
                      className="hover:text-brand transition-colors max-w-[120px] truncate"
                      title={seg.fullPath}
                      aria-current={seg.stackIndex === pathStack.length - 1 ? 'page' : undefined}
                    >
                      {seg.label}
                    </button>
                  </span>
                ))}
              </nav>

              {/* File list */}
              <div className="flex-1 overflow-y-auto">
                <RemoteFileList
                  backendId={selectedBackendId}
                  path={currentPath}
                  onNavigate={handleNavigate}
                />
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
