import { useState, useEffect, useCallback } from 'react';
import { Button } from '../components/ui/Button';
import { ghostdriveApi } from '../services/wails';
import type { AppConfig, CacheStats } from '../types/ghostdrive';

/** True when the value looks like a Windows drive letter (e.g. "G" or "G:"). */
const isDriveLetter = (v: string): boolean => /^[A-Za-z]:?$/.test(v.trim());

interface ConfigPageProps {
  appConfig: AppConfig | null;
  onConfigChange: (cfg: AppConfig) => void;
}

export function ConfigPage({ appConfig, onConfigChange }: ConfigPageProps) {
  const handleSave = async (updates: Partial<AppConfig>) => {
    if (!appConfig) return;
    const next: AppConfig = { ...appConfig, ...updates };
    await ghostdriveApi.saveConfig(next);
    onConfigChange(next);
  };

  return (
    <div className="h-full overflow-y-auto">
      <div className="flex flex-col gap-5 p-3">

        {/* ── Startup ───────────────────────────────────────── */}
        {appConfig && (
          <section>
            <h3 className="text-xs font-semibold text-gray-500 uppercase tracking-wide mb-2">
              Démarrage
            </h3>
            <div className="space-y-1">
              <ToggleRow
                label="Démarrer avec Windows"
                checked={appConfig.autoStart}
                onChange={v => handleSave({ autoStart: v })}
              />
              <ToggleRow
                label="Démarrer minimisé"
                checked={appConfig.startMinimized}
                onChange={v => handleSave({ startMinimized: v })}
              />
            </div>
          </section>
        )}

        {/* ── Drive virtuel ─────────────────────────────────── */}
        {appConfig && (
          <MountPointSection appConfig={appConfig} onSave={handleSave} />
        )}

        {/* ── Cache ─────────────────────────────────────────── */}
        <CacheSection appConfig={appConfig} onSave={handleSave} />

      </div>
    </div>
  );
}

// ── Sub-components ─────────────────────────────────────────

/**
 * MountPointSection — Point de montage du drive virtuel GhD:
 *
 * Si la valeur stockée est une lettre Windows (ex: "G:") → affiche un <select>
 * peuplé par GetAvailableDriveLetters().
 * Sinon → affiche un <input type="text"> pour un chemin libre.
 */
function MountPointSection({
  appConfig,
  onSave,
}: {
  appConfig: AppConfig;
  onSave: (updates: Partial<AppConfig>) => void;
}) {
  const rawValue = appConfig.mountPoint ?? appConfig.driveLetter ?? '';
  // Normalize to "X:" form if user stored just "X"
  const normalize = (v: string) =>
    isDriveLetter(v) && !v.endsWith(':') ? `${v.toUpperCase()}:` : v;

  const initialIsLetter = isDriveLetter(rawValue);
  const [mode, setMode] = useState<'letter' | 'path'>(
    initialIsLetter ? 'letter' : 'path',
  );
  const [letters, setLetters] = useState<string[]>([]);
  const [lettersLoading, setLettersLoading] = useState(false);
  const [selectedLetter, setSelectedLetter] = useState<string>(
    initialIsLetter ? normalize(rawValue) : 'G:',
  );

  const loadLetters = useCallback(async () => {
    setLettersLoading(true);
    try {
      const avail = await ghostdriveApi.getAvailableDriveLetters();
      setLetters(avail ?? []);
      // If current letter is in the list, keep it; otherwise fall back to first available
      if (avail && avail.length > 0 && !avail.includes(selectedLetter)) {
        setSelectedLetter(avail[0]);
      }
    } catch {
      setLetters([]);
    } finally {
      setLettersLoading(false);
    }
  }, [selectedLetter]);

  useEffect(() => {
    if (mode === 'letter') {
      loadLetters();
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode]);

  const handleModeToggle = () => {
    const next = mode === 'letter' ? 'path' : 'letter';
    setMode(next);
  };

  return (
    <section>
      <div className="flex items-center justify-between mb-2">
        <h3 className="text-xs font-semibold text-gray-500 uppercase tracking-wide">
          Drive virtuel (GhD:)
        </h3>
        {/* Mode toggle */}
        <button
          type="button"
          onClick={handleModeToggle}
          className="text-xs text-brand hover:underline"
          aria-label={
            mode === 'letter'
              ? 'Passer en mode chemin libre'
              : 'Passer en mode lettre de lecteur'
          }
        >
          {mode === 'letter' ? 'Utiliser un chemin' : 'Utiliser une lettre'}
        </button>
      </div>

      <div className="flex items-center justify-between py-1 gap-3">
        <label
          htmlFor="mount-point-config"
          className="text-sm text-gray-800 shrink-0"
        >
          Point de montage
        </label>

        {mode === 'letter' ? (
          <select
            id="mount-point-config"
            value={selectedLetter}
            disabled={lettersLoading || letters.length === 0}
            onChange={e => {
              setSelectedLetter(e.target.value);
              onSave({ mountPoint: e.target.value });
            }}
            className="flex-1 min-w-0 rounded border border-surface-border px-2 py-1 text-sm
              focus:outline-none focus:ring-2 focus:ring-brand bg-white"
            aria-label="Lettre de lecteur Windows pour le drive GhD:"
          >
            {lettersLoading && <option value="">Chargement...</option>}
            {!lettersLoading && letters.length === 0 && (
              <option value="">Aucune lettre disponible</option>
            )}
            {letters.map(l => (
              <option key={l} value={l}>
                {l}
              </option>
            ))}
          </select>
        ) : (
          <input
            id="mount-point-config"
            type="text"
            defaultValue={!initialIsLetter ? rawValue : ''}
            placeholder="C:\\GhostDrive\\GhD\\"
            onBlur={e =>
              onSave({ mountPoint: e.target.value.trim() || undefined })
            }
            className="flex-1 min-w-0 rounded border border-surface-border px-2 py-1 text-sm
              focus:outline-none focus:ring-2 focus:ring-brand"
            aria-label="Chemin complet du point de montage du drive virtuel GhD:"
          />
        )}
      </div>

      <p className="text-xs text-gray-400 mt-1">
        {mode === 'letter'
          ? 'Lettre Windows disponible (ex : G:).'
          : 'Chemin complet sur le disque (ex : C:\\GhostDrive\\GhD\\).'}
      </p>
    </section>
  );
}

function ToggleRow({
  label,
  checked,
  onChange,
}: {
  label: string;
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <label className="flex items-center justify-between py-1 cursor-pointer">
      <span className="text-sm text-gray-800">{label}</span>
      <input
        type="checkbox"
        checked={checked}
        onChange={e => onChange(e.target.checked)}
        className="accent-brand w-4 h-4"
        aria-label={label}
      />
    </label>
  );
}

function CacheSection({
  appConfig,
  onSave,
}: {
  appConfig: AppConfig | null;
  onSave: (updates: Partial<AppConfig>) => void;
}) {
  const [stats, setStats] = useState<CacheStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [clearing, setClearing] = useState(false);

  useEffect(() => {
    ghostdriveApi.getCacheStats()
      .then(s => { setStats(s); setLoading(false); })
      .catch(() => setLoading(false));
  }, []);

  const handleClear = async () => {
    if (!window.confirm(
      'Vider le cache local ? Les fichiers placeholders devront être re-téléchargés.',
    )) return;
    setClearing(true);
    try {
      await ghostdriveApi.clearCache();
      const s = await ghostdriveApi.getCacheStats();
      setStats(s);
    } finally {
      setClearing(false);
    }
  };

  return (
    <section>
      <h3 className="text-xs font-semibold text-gray-500 uppercase tracking-wide mb-2">
        Cache local
      </h3>

      {appConfig && (
        <div className="space-y-1 mb-3">
          <ToggleRow
            label="Activer le cache"
            checked={appConfig.cacheEnabled}
            onChange={v => onSave({ cacheEnabled: v })}
          />
          {appConfig.cacheEnabled && (
            <div className="flex items-center justify-between py-1">
              <label htmlFor="cache-max-config" className="text-sm text-gray-800">
                Taille max (Mo)
              </label>
              <input
                id="cache-max-config"
                type="number"
                min={64}
                max={102400}
                step={64}
                defaultValue={appConfig.cacheSizeMaxMB}
                onBlur={e => onSave({ cacheSizeMaxMB: Number(e.target.value) })}
                className="w-24 rounded border border-surface-border px-2 py-1 text-sm text-right
                  focus:outline-none focus:ring-2 focus:ring-brand"
                aria-label="Taille maximale du cache en Mo"
              />
            </div>
          )}
        </div>
      )}

      {loading ? (
        <p className="text-sm text-gray-400 py-2">Chargement...</p>
      ) : stats ? (
        <dl className="space-y-1 mb-3">
          <StatRow label="Taille utilisée"   value={`${stats.sizeMB} Mo`} />
          <StatRow label="Fichiers en cache"  value={String(stats.fileCount)} />
          <StatRow label="Limite configurée"  value={`${stats.maxSizeMB} Mo`} />
        </dl>
      ) : (
        <p className="text-sm text-gray-400 mb-3">Statistiques indisponibles.</p>
      )}

      <Button
        variant="secondary"
        onClick={handleClear}
        disabled={clearing || !stats || stats.fileCount === 0}
        className="w-full justify-center"
      >
        {clearing ? 'Vidage...' : 'Vider le cache'}
      </Button>
    </section>
  );
}

function StatRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between py-0.5">
      <dt className="text-sm text-gray-600">{label}</dt>
      <dd className="text-sm font-medium text-gray-900">{value}</dd>
    </div>
  );
}
