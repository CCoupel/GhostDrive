import { useState, useEffect } from 'react';
import { Button } from '../components/ui/Button';
import { ghostdriveApi } from '../services/wails';
import type { AppConfig, CacheStats } from '../types/ghostdrive';

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

        {/* ── Cache ─────────────────────────────────────────── */}
        <CacheSection appConfig={appConfig} onSave={handleSave} />

      </div>
    </div>
  );
}

// ── Sub-components ─────────────────────────────────────────

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
