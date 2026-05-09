import { useState, useEffect } from 'react';
import { Github, ExternalLink, RefreshCw, CheckCircle, AlertCircle, Cpu, Puzzle } from 'lucide-react';
import { Button } from '../components/ui/Button';
import { ghostdriveApi } from '../services/wails';
import type { AppConfig, BuildInfo, PluginBuildInfo } from '../types/ghostdrive';

interface AboutPageProps {
  appConfig: AppConfig | null;
}

type UpdateState =
  | { status: 'idle' }
  | { status: 'checking' }
  | { status: 'up-to-date' }
  | { status: 'available'; version: string }
  | { status: 'error'; message: string };

const GITHUB_REPO = 'CCoupel/GhostDrive';

/** Format an RFC3339 timestamp to a short date (YYYY-MM-DD), or return "unknown" as-is. */
function formatBuildTime(raw: string): string {
  if (!raw || raw === 'unknown') return 'unknown';
  try {
    return new Date(raw).toISOString().slice(0, 10);
  } catch {
    return raw;
  }
}

// ── Moteur section ─────────────────────────────────────────────────────────────

interface EngineInfoProps {
  info: BuildInfo | null;
  error: string | null;
}

function EngineInfo({ info, error }: EngineInfoProps) {
  return (
    <section aria-labelledby="about-engine-heading">
      <h3
        id="about-engine-heading"
        className="flex items-center gap-1.5 text-xs font-semibold text-gray-500 uppercase tracking-wide mb-2"
      >
        <Cpu size={12} aria-hidden="true" />
        Moteur
      </h3>

      {error && (
        <p className="flex items-center gap-1 text-xs text-status-error">
          <AlertCircle size={11} />
          {error}
        </p>
      )}

      {info && (
        <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-xs">
          <dt className="text-gray-400 whitespace-nowrap">Version</dt>
          <dd className="font-mono text-gray-800">
            GhostDrive v{info.version}
          </dd>

          <dt className="text-gray-400 whitespace-nowrap">Commit</dt>
          <dd className="font-mono text-gray-800">{info.commit}</dd>

          <dt className="text-gray-400 whitespace-nowrap">Compilé le</dt>
          <dd className="font-mono text-gray-800">{formatBuildTime(info.buildTime)}</dd>

          <dt className="text-gray-400 whitespace-nowrap">Go</dt>
          <dd className="font-mono text-gray-800">{info.goVersion}</dd>
        </dl>
      )}

      {!info && !error && (
        <p className="text-xs text-gray-400 italic">Chargement…</p>
      )}
    </section>
  );
}

// ── Plugins section ────────────────────────────────────────────────────────────

interface PluginsInfoProps {
  plugins: PluginBuildInfo[] | null;
  error: string | null;
}

function PluginsInfo({ plugins, error }: PluginsInfoProps) {
  return (
    <section aria-labelledby="about-plugins-heading">
      <h3
        id="about-plugins-heading"
        className="flex items-center gap-1.5 text-xs font-semibold text-gray-500 uppercase tracking-wide mb-2"
      >
        <Puzzle size={12} aria-hidden="true" />
        Plugins chargés
      </h3>

      {error && (
        <p className="flex items-center gap-1 text-xs text-status-error">
          <AlertCircle size={11} />
          {error}
        </p>
      )}

      {plugins && plugins.length === 0 && (
        <p className="text-xs text-gray-400 italic">Aucun plugin chargé.</p>
      )}

      {plugins && plugins.length > 0 && (
        <ul className="flex flex-col gap-2" role="list">
          {plugins.map((p) => (
            <li
              key={p.path}
              className="bg-surface-secondary rounded p-2 text-xs"
            >
              <div className="flex items-baseline gap-2 mb-0.5">
                <span className="font-semibold text-gray-800">{p.name}</span>
                <span className="font-mono text-gray-500">v{p.version}</span>
                <span className="font-mono text-gray-400">commit: {p.commit}</span>
              </div>
              <p
                className="font-mono text-gray-400 truncate"
                title={p.path}
              >
                {p.path}
              </p>
            </li>
          ))}
        </ul>
      )}

      {!plugins && !error && (
        <p className="text-xs text-gray-400 italic">Chargement…</p>
      )}
    </section>
  );
}

// ── Main page ─────────────────────────────────────────────────────────────────

export function AboutPage({ appConfig }: AboutPageProps) {
  const [updateState, setUpdateState] = useState<UpdateState>({ status: 'idle' });
  const [buildInfo, setBuildInfo] = useState<BuildInfo | null>(null);
  const [buildInfoError, setBuildInfoError] = useState<string | null>(null);
  const [plugins, setPlugins] = useState<PluginBuildInfo[] | null>(null);
  const [pluginsError, setPluginsError] = useState<string | null>(null);

  useEffect(() => {
    let mounted = true;

    ghostdriveApi.getBuildInfo()
      .then((info) => { if (mounted) setBuildInfo(info); })
      .catch((err: unknown) => {
        if (mounted) setBuildInfoError(
          err instanceof Error ? err.message : 'Impossible de récupérer les infos moteur',
        );
      });

    ghostdriveApi.getLoadedPlugins()
      .then((list) => { if (mounted) setPlugins(list); })
      .catch((err: unknown) => {
        if (mounted) setPluginsError(
          err instanceof Error ? err.message : 'Impossible de récupérer les plugins',
        );
      });

    return () => { mounted = false; };
  }, []);

  const handleCheckUpdates = async () => {
    setUpdateState({ status: 'checking' });
    try {
      const res = await fetch(
        `https://api.github.com/repos/${GITHUB_REPO}/releases/latest`,
        { headers: { Accept: 'application/vnd.github.v3+json' } },
      );
      if (!res.ok) throw new Error(`GitHub API: ${res.status}`);
      const data = await res.json() as { tag_name: string };
      const latest = data.tag_name.replace(/^v/, '');
      const current = appConfig?.version ?? '0.0.0';
      if (latest === current) {
        setUpdateState({ status: 'up-to-date' });
      } else {
        setUpdateState({ status: 'available', version: data.tag_name });
      }
    } catch (e) {
      setUpdateState({
        status: 'error',
        message: e instanceof Error ? e.message : 'Impossible de vérifier',
      });
    }
  };

  return (
    <div className="h-full overflow-y-auto">
      <div className="flex flex-col gap-5 p-3">

        {/* ── App identity ──────────────────────────────────── */}
        <section>
          <h3 className="text-xs font-semibold text-gray-500 uppercase tracking-wide mb-2">
            GhostDrive
          </h3>
          <p className="text-sm text-gray-700 font-medium">
            Version {appConfig?.version ?? '—'}
          </p>
          <a
            href={`https://github.com/${GITHUB_REPO}`}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1 text-xs text-brand hover:underline mt-1"
          >
            <Github size={11} />
            github.com/{GITHUB_REPO}
            <ExternalLink size={10} />
          </a>
        </section>

        {/* ── Engine build info ──────────────────────────────── */}
        <EngineInfo info={buildInfo} error={buildInfoError} />

        {/* ── Loaded plugins ─────────────────────────────────── */}
        <PluginsInfo plugins={plugins} error={pluginsError} />

        {/* ── Update check ──────────────────────────────────── */}
        <section>
          <h3 className="text-xs font-semibold text-gray-500 uppercase tracking-wide mb-2">
            Mises à jour
          </h3>

          <Button
            variant="secondary"
            onClick={handleCheckUpdates}
            disabled={updateState.status === 'checking'}
            className="w-full justify-center"
          >
            <RefreshCw
              size={13}
              className={updateState.status === 'checking' ? 'animate-spin' : ''}
            />
            {updateState.status === 'checking' ? 'Vérification...' : 'Vérifier les mises à jour'}
          </Button>

          {updateState.status === 'up-to-date' && (
            <p className="flex items-center gap-1.5 text-xs text-status-idle mt-2">
              <CheckCircle size={12} />
              GhostDrive est à jour.
            </p>
          )}

          {updateState.status === 'available' && (
            <p className="flex items-center gap-1.5 text-xs text-brand mt-2">
              <CheckCircle size={12} />
              Mise à jour disponible : <strong>{updateState.version}</strong>
              <a
                href={`https://github.com/${GITHUB_REPO}/releases/latest`}
                target="_blank"
                rel="noopener noreferrer"
                className="underline ml-0.5"
              >
                Télécharger
              </a>
            </p>
          )}

          {updateState.status === 'error' && (
            <p className="flex items-center gap-1.5 text-xs text-status-error mt-2">
              <AlertCircle size={12} />
              {updateState.message}
            </p>
          )}
        </section>

      </div>
    </div>
  );
}
