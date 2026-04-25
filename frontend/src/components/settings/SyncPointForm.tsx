import { useState, useEffect, useMemo } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { CheckCircle, XCircle, AlertTriangle } from 'lucide-react';
import { Input } from '../ui/Input';
import { Button } from '../ui/Button';
import { ghostdriveApi } from '../../services/wails';
import { SelectDirectory, GetGhostDriveRoot } from '../../../wailsjs/go/app/App';
import type { BackendConfig, BackendStatus, BackendType } from '../../types/ghostdrive';
import { formatSpace } from '../../utils/formatBytes';

// ── Constants ─────────────────────────────────────────────────────────────────
const WINDOWS_INVALID_CHARS_RE = /[\\/:*?"<>|]/;
const GHOST_DRIVE_ROOT = 'C:\\GhostDrive';

// ── Schemas ───────────────────────────────────────────────────────────────────

/** Zone LOCAL — partagée pour tous les types de backend */
const localZoneSchema = z.object({
  name: z.string()
    .min(1, 'Requis')
    .max(64, 'Max 64 caractères')
    .refine(v => !WINDOWS_INVALID_CHARS_RE.test(v), 'Caractères interdits (\\ / : * ? " < > |)'),
  localPathMode: z.enum(['auto', 'manual']),
  manualPath:    z.string().optional(),
}).superRefine((data, ctx) => {
  if (data.localPathMode === 'manual' && !data.manualPath?.trim()) {
    ctx.addIssue({
      code: z.ZodIssueCode.custom,
      message: 'Le chemin est requis en mode Manuel',
      path: ['manualPath'],
    });
  }
});

/** Zone REMOTE — WebDAV */
const webdavRemoteSchema = z.object({
  url:        z.string().url('URL invalide'),
  username:   z.string().min(1, 'Requis'),
  password:   z.string().min(1, 'Requis'),
  insecure:   z.boolean().optional(),
  remotePath: z.string().min(1, 'Requis').refine(v => v.startsWith('/'), 'Doit commencer par /'),
});

/** Zone REMOTE — MooseFS */
const moosefsRemoteSchema = z.object({
  master:     z.string().min(1, 'Requis'),
  port:       z.string().optional(),
  mountPath:  z.string().min(1, 'Requis').refine(v => v.startsWith('/'), 'Doit commencer par /'),
  remotePath: z.string().min(1, 'Requis').refine(v => v.startsWith('/'), 'Doit commencer par /'),
});

/** Zone REMOTE — Local */
const localRemoteSchema = z.object({
  rootPath: z.string().min(1, 'Le dossier source est requis'),
});

type LocalZoneForm     = z.infer<typeof localZoneSchema>;
type WebDAVRemoteForm  = z.infer<typeof webdavRemoteSchema>;
type MooseFSRemoteForm = z.infer<typeof moosefsRemoteSchema>;
type LocalRemoteForm   = z.infer<typeof localRemoteSchema>;
type AnyRemoteForm     = WebDAVRemoteForm | MooseFSRemoteForm | LocalRemoteForm;

// ── Types ─────────────────────────────────────────────────────────────────────

type TestState =
  | { status: 'idle' }
  | { status: 'testing' }
  | { status: 'ok'; result: BackendStatus }
  | { status: 'fail'; message: string };

interface SyncPointFormProps {
  onSuccess:       (config: BackendConfig) => void;
  onCancel:        () => void;
  /** Noms existants pour la validation d'unicité (insensible à la casse) */
  existingNames?:  string[];
  /** Warning non bloquant affiché si le rootPath remote est déjà utilisé */
  warningMessage?: string;
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function labelFor(type: BackendType): string {
  switch (type) {
    case 'webdav':  return 'WebDAV';
    case 'moosefs': return 'MooseFS';
    case 'local':   return 'Local';
    default:        return type;
  }
}

function buildDraft(
  localZone: LocalZoneForm,
  remote: AnyRemoteForm,
  backendType: BackendType,
): Omit<BackendConfig, 'id'> {
  const localPath = localZone.localPathMode === 'manual'
    ? (localZone.manualPath ?? '')
    : '';

  const base: Omit<BackendConfig, 'id' | 'params' | 'remotePath'> = {
    name:       localZone.name,
    type:       backendType,
    enabled:    true,
    autoSync:   false,
    localPath,
    syncDir:    localPath,
  };

  if (backendType === 'webdav') {
    const d = remote as WebDAVRemoteForm;
    return {
      ...base,
      remotePath: d.remotePath,
      params: {
        url:      d.url,
        username: d.username,
        password: d.password,
        ...(d.insecure ? { insecure: 'true' } : {}),
      },
    };
  }
  if (backendType === 'moosefs') {
    const d = remote as MooseFSRemoteForm;
    return {
      ...base,
      remotePath: d.remotePath,
      params: {
        master:    d.master,
        mountPath: d.mountPath,
        ...(d.port ? { port: d.port } : {}),
      },
    };
  }
  // local
  const d = remote as LocalRemoteForm;
  return { ...base, remotePath: '/', params: { rootPath: d.rootPath } };
}

// ── Component ─────────────────────────────────────────────────────────────────

export function SyncPointForm({
  onSuccess,
  onCancel,
  existingNames,
  warningMessage,
}: SyncPointFormProps) {
  const [availableTypes, setAvailableTypes] = useState<BackendType[]>([]);
  const [typesLoading, setTypesLoading]     = useState(true);
  const [backendType, setBackendType]       = useState<BackendType>('webdav');
  const [testState, setTestState]           = useState<TestState>({ status: 'idle' });
  const [submitting, setSubmitting]         = useState(false);
  const [submitError, setSubmitError]       = useState<string | null>(null);
  const [ghostDriveRoot, setGhostDriveRoot] = useState(GHOST_DRIVE_ROOT);

  useEffect(() => {
    ghostdriveApi.getAvailableBackendTypes()
      .then(types => {
        setAvailableTypes(types as BackendType[]);
        if (types.length > 0) setBackendType(types[0] as BackendType);
      })
      .catch(() => setAvailableTypes([]))
      .finally(() => setTypesLoading(false));
  }, []);

  useEffect(() => {
    GetGhostDriveRoot()
      .then(r => { if (r) setGhostDriveRoot(r); })
      .catch(() => { /* conserve la valeur par défaut */ });
  }, []);

  // Zone LOCAL (partagée)
  const localZoneForm = useForm<LocalZoneForm>({
    resolver: zodResolver(localZoneSchema),
    defaultValues: { localPathMode: 'auto' },
  });

  // Zones REMOTE (une par type)
  const webdavRemoteForm  = useForm<WebDAVRemoteForm>({ resolver: zodResolver(webdavRemoteSchema) });
  const moosefsRemoteForm = useForm<MooseFSRemoteForm>({ resolver: zodResolver(moosefsRemoteSchema) });
  const localRemoteForm   = useForm<LocalRemoteForm>({ resolver: zodResolver(localRemoteSchema) });

  // Surveillances temps réel
  const nameValue     = localZoneForm.watch('name') ?? '';
  const localPathMode = localZoneForm.watch('localPathMode') ?? 'auto';

  // Aperçus de chemin
  const safeName    = nameValue.trim() || '<nom>';
  const ghdPreview  = `GhD:\\${safeName}\\`;
  const autoPreview = `${ghostDriveRoot}\\${safeName}\\`;

  // Liste noms en minuscule pour la validation unicité
  const existingNamesLower = useMemo(
    () => (existingNames ?? []).map(n => n.toLowerCase()),
    [existingNames],
  );

  // Feedback unicité du nom en temps réel.
  // IMPORTANT: localZoneForm (useForm return value) is a new object reference on every render
  // in react-hook-form v7. Including it in the dep array would re-run this effect on every
  // render and, when isDuplicate=true, create an infinite loop (setError → re-render → effect
  // → setError …) that freezes the entire app (JS thread → WebView2 message pump → systray).
  // Fix: destructure stable method refs, guard setError/clearErrors to avoid redundant calls.
  const { setError: setNameError, clearErrors: clearNameErrors } = localZoneForm;
  const nameErrorMsg = localZoneForm.formState.errors.name?.message;
  const DUPE_MSG = 'Un backend avec ce nom existe déjà';

  useEffect(() => {
    if (!nameValue) return;
    const isDuplicate = existingNamesLower.includes(nameValue.toLowerCase());
    if (isDuplicate && nameErrorMsg !== DUPE_MSG) {
      setNameError('name', { message: DUPE_MSG });
    } else if (!isDuplicate && nameErrorMsg === DUPE_MSG) {
      clearNameErrors('name');
    }
    // setNameError / clearNameErrors are stable refs from the RHF control ref.
    // nameErrorMsg is included so the effect re-runs when the error is cleared/set,
    // but the guards above prevent it from re-triggering another setError call.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [nameValue, existingNamesLower, nameErrorMsg]);

  // ── Actions ──────────────────────────────────────────────────────────────

  const switchType = (t: BackendType) => {
    setBackendType(t);
    setTestState({ status: 'idle' });
    setSubmitError(null);
  };

  /** Valide les deux zones et retourne les données, ou null si invalide */
  const validateAndGetData = async (): Promise<{ localZone: LocalZoneForm; remote: AnyRemoteForm } | null> => {
    const localValid = await localZoneForm.trigger();

    // Unicité du nom (vérification après zod)
    const name = localZoneForm.getValues('name');
    if (name && existingNamesLower.includes(name.toLowerCase())) {
      localZoneForm.setError('name', { message: 'Un backend avec ce nom existe déjà' });
      return null;
    }

    const remoteValid = await (
      backendType === 'webdav'  ? webdavRemoteForm.trigger() :
      backendType === 'moosefs' ? moosefsRemoteForm.trigger() :
      localRemoteForm.trigger()
    );

    if (!localValid || !remoteValid) return null;

    const localZone = localZoneForm.getValues();
    const remote = (
      backendType === 'webdav'  ? webdavRemoteForm.getValues() :
      backendType === 'moosefs' ? moosefsRemoteForm.getValues() :
      localRemoteForm.getValues()
    ) as AnyRemoteForm;

    return { localZone, remote };
  };

  const handleTest = async () => {
    const data = await validateAndGetData();
    if (!data) return;
    setTestState({ status: 'testing' });
    try {
      const result = await ghostdriveApi.testBackendConnection(
        { ...buildDraft(data.localZone, data.remote, backendType), id: '' } as BackendConfig,
      );
      setTestState({ status: 'ok', result });
    } catch (e) {
      setTestState({ status: 'fail', message: (e as Error).message ?? 'Connexion impossible' });
    }
  };

  const handleAdd = async () => {
    const data = await validateAndGetData();
    if (!data) return;
    setSubmitError(null);
    setSubmitting(true);
    try {
      const created = await ghostdriveApi.addBackend(
        { ...buildDraft(data.localZone, data.remote, backendType), id: '' } as BackendConfig,
      );
      onSuccess(created);
    } catch (e) {
      setSubmitError((e as Error).message ?? "Erreur lors de l'ajout.");
    } finally {
      setSubmitting(false);
    }
  };

  // Sélecteurs de dossiers natifs
  const handleBrowseManualPath = async () => {
    const dir = await SelectDirectory();
    if (dir) localZoneForm.setValue('manualPath', dir, { shouldValidate: true });
  };

  const handleBrowseRootPath = async () => {
    const dir = await SelectDirectory();
    if (dir) localRemoteForm.setValue('rootPath', dir, { shouldValidate: true });
  };

  // ── Render ────────────────────────────────────────────────────────────────

  if (typesLoading) {
    return <p className="text-sm text-gray-400 text-center py-6">Chargement des plugins...</p>;
  }

  if (availableTypes.length === 0) {
    return (
      <div className="text-center py-6">
        <p className="text-sm text-gray-500 mb-1">Aucun plugin de stockage disponible.</p>
        <p className="text-xs text-gray-400">Aucun backend de stockage configuré.</p>
        <Button variant="secondary" onClick={onCancel} className="mt-4">Fermer</Button>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4">

      {/* Sélecteur de type */}
      <div>
        <p className="text-xs font-medium text-gray-700 mb-1.5">Type de backend</p>
        <div className="flex gap-2" role="group" aria-label="Type de backend">
          {availableTypes.map(t => (
            <button
              key={t}
              type="button"
              onClick={() => switchType(t)}
              className={`flex-1 py-1.5 text-sm rounded border transition-colors
                ${backendType === t
                  ? 'border-brand bg-brand-light text-brand font-medium'
                  : 'border-surface-border text-gray-600 hover:bg-surface-secondary'
                }`}
              aria-pressed={backendType === t}
            >
              {labelFor(t)}
            </button>
          ))}
        </div>
      </div>

      {/* ── Zone LOCAL ─────────────────────────────────────────────────────── */}
      <section aria-labelledby="zone-local-label">
        <div className="flex items-center gap-2 mb-3">
          <span
            id="zone-local-label"
            className="text-xs font-semibold text-gray-400 uppercase tracking-wide whitespace-nowrap"
          >
            Local
          </span>
          <div className="h-px flex-1 bg-gray-200" aria-hidden="true" />
        </div>

        <div className="flex flex-col gap-3">

          {/* Nom du backend */}
          <div>
            <Input
              label="Nom du backend"
              required
              placeholder="MonNAS, DisqueFamille..."
              {...localZoneForm.register('name')}
              error={localZoneForm.formState.errors.name?.message}
            />
            <p className="text-xs text-gray-400 mt-0.5">
              Aperçu :&nbsp;
              <span className="font-mono text-brand">{ghdPreview}</span>
            </p>
          </div>

          {/* Mode sync-point */}
          <fieldset>
            <legend className="text-xs font-medium text-gray-700 mb-1.5">
              Dossier local
            </legend>
            <div className="flex flex-col gap-2">
              <label className="flex items-start gap-2 cursor-pointer">
                <input
                  type="radio"
                  value="auto"
                  className="accent-brand mt-0.5"
                  {...localZoneForm.register('localPathMode')}
                />
                <span className="text-sm text-gray-700">
                  Auto —&nbsp;
                  <span className="font-mono text-xs text-gray-500">{autoPreview}</span>
                </span>
              </label>
              <label className="flex items-center gap-2 cursor-pointer">
                <input
                  type="radio"
                  value="manual"
                  className="accent-brand"
                  {...localZoneForm.register('localPathMode')}
                />
                <span className="text-sm text-gray-700">Manuel</span>
              </label>
            </div>

            {localPathMode === 'manual' && (
              <div className="mt-2 flex flex-col gap-1">
                <label htmlFor="manualPath-input" className="text-xs font-medium text-gray-700">
                  Chemin <span className="text-red-500">*</span>
                </label>
                <div className="flex gap-2">
                <Input
                  id="manualPath-input"
                  placeholder="C:\sync\MonDossier"
                  {...localZoneForm.register('manualPath')}
                  error={localZoneForm.formState.errors.manualPath?.message}
                  className="flex-1"
                />
                <Button
                  type="button"
                  variant="secondary"
                  onClick={() => void handleBrowseManualPath()}
                  className="shrink-0"
                >
                  Parcourir…
                </Button>
                </div>
              </div>
            )}
          </fieldset>
        </div>
      </section>

      {/* ── Zone REMOTE ────────────────────────────────────────────────────── */}
      <section aria-labelledby="zone-remote-label">
        <div className="flex items-center gap-2 mb-3">
          <span
            id="zone-remote-label"
            className="text-xs font-semibold text-gray-400 uppercase tracking-wide whitespace-nowrap"
          >
            Remote — {labelFor(backendType)}
          </span>
          <div className="h-px flex-1 bg-gray-200" aria-hidden="true" />
        </div>

        {backendType === 'webdav' && (
          <div className="flex flex-col gap-3">
            <Input
              label="URL WebDAV" required placeholder="https://nas.local/dav"
              {...webdavRemoteForm.register('url')}
              error={webdavRemoteForm.formState.errors.url?.message}
            />
            <Input
              label="Utilisateur" required autoComplete="username"
              {...webdavRemoteForm.register('username')}
              error={webdavRemoteForm.formState.errors.username?.message}
            />
            <Input
              label="Mot de passe" type="password" required autoComplete="new-password"
              {...webdavRemoteForm.register('password')}
              error={webdavRemoteForm.formState.errors.password?.message}
            />
            <label className="flex items-center gap-2 cursor-pointer">
              <input
                type="checkbox"
                {...webdavRemoteForm.register('insecure')}
                className="accent-brand w-4 h-4"
              />
              <span className="text-sm text-gray-700">Accepter les certificats auto-signés</span>
            </label>
            <Input
              label="Chemin distant" required placeholder="/GhostDrive"
              hint="Chemin racine sur le serveur WebDAV"
              {...webdavRemoteForm.register('remotePath')}
              error={webdavRemoteForm.formState.errors.remotePath?.message}
            />
          </div>
        )}

        {backendType === 'moosefs' && (
          <div className="flex flex-col gap-3">
            <Input
              label="Adresse du Master" required placeholder="192.168.1.1"
              {...moosefsRemoteForm.register('master')}
              error={moosefsRemoteForm.formState.errors.master?.message}
            />
            <Input
              label="Port" placeholder="9421" hint="Défaut : 9421"
              {...moosefsRemoteForm.register('port')}
            />
            <Input
              label="Chemin de montage FUSE" required placeholder="/mnt/moosefs"
              {...moosefsRemoteForm.register('mountPath')}
              error={moosefsRemoteForm.formState.errors.mountPath?.message}
            />
            <Input
              label="Chemin distant" required placeholder="/GhostDrive"
              hint="Chemin racine sur MooseFS"
              {...moosefsRemoteForm.register('remotePath')}
              error={moosefsRemoteForm.formState.errors.remotePath?.message}
            />
          </div>
        )}

        {backendType === 'local' && (
          <div className="flex flex-col gap-1">
            <label htmlFor="rootPath-input" className="text-xs font-medium text-gray-700">
              Dossier source <span className="text-red-500">*</span>
            </label>
            <div className="flex gap-2">
              <Input
                id="rootPath-input"
                placeholder="D:\Photos\..."
                {...localRemoteForm.register('rootPath')}
                error={localRemoteForm.formState.errors.rootPath?.message}
                className="flex-1"
              />
              <Button
                type="button"
                variant="secondary"
                onClick={() => void handleBrowseRootPath()}
                className="shrink-0"
              >
                Parcourir…
              </Button>
            </div>
            <p className="text-xs text-gray-400">Répertoire source à synchroniser</p>
          </div>
        )}
      </section>

      {/* Warning rootPath dupliqué (non bloquant) */}
      {warningMessage && (
        <div
          role="alert"
          className="flex items-start gap-1.5 text-xs text-amber-700 bg-amber-50 border border-amber-200 rounded px-2 py-1.5"
        >
          <AlertTriangle size={13} className="mt-0.5 shrink-0" />
          <span>{warningMessage}</span>
        </div>
      )}

      {/* Feedback test de connexion */}
      {testState.status === 'ok' && (
        <div className="flex items-center gap-1.5 text-xs text-status-idle bg-green-50 rounded px-2 py-1.5">
          <CheckCircle size={13} />
          <span>
            Connexion réussie — Libre&nbsp;: {formatSpace(testState.result.freeSpace)}
            {' / '}Total&nbsp;: {formatSpace(testState.result.totalSpace)}
          </span>
        </div>
      )}
      {testState.status === 'fail' && (
        <div
          role="alert"
          className="flex items-center gap-1.5 text-xs text-red-500 bg-red-50 rounded px-2 py-1.5"
        >
          <XCircle size={13} />
          <span>{testState.message}</span>
        </div>
      )}
      {submitError && (
        <p role="alert" className="text-xs text-red-500 bg-red-50 rounded px-2 py-1.5">
          {submitError}
        </p>
      )}

      {/* Boutons d'action */}
      <div className="flex justify-end gap-2">
        <Button variant="secondary" type="button" onClick={onCancel}>
          Annuler
        </Button>
        <Button
          variant="ghost"
          type="button"
          disabled={testState.status === 'testing'}
          onClick={() => void handleTest()}
        >
          {testState.status === 'testing' ? 'Test...' : 'Tester la connexion'}
        </Button>
        <Button
          variant="primary"
          type="button"
          disabled={submitting}
          onClick={() => void handleAdd()}
        >
          {submitting ? 'Ajout...' : 'Ajouter'}
        </Button>
      </div>
    </div>
  );
}
