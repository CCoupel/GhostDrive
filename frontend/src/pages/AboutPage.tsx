import { useState } from 'react';
import { Github, ExternalLink, RefreshCw, CheckCircle, AlertCircle } from 'lucide-react';
import { Button } from '../components/ui/Button';
import type { AppConfig } from '../types/ghostdrive';

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

export function AboutPage({ appConfig }: AboutPageProps) {
  const [updateState, setUpdateState] = useState<UpdateState>({ status: 'idle' });

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
