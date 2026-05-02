import { useState, useEffect, useMemo } from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { CheckCircle, XCircle, AlertTriangle } from 'lucide-react';
import { Input } from '../ui/Input';
import { Button } from '../ui/Button';
import { ghostdriveApi } from '../../services/wails';
import { SelectDirectory, GetGhostDriveRoot } from '../../../wailsjs/go/app/App';
import type {
  BackendConfig,
  BackendStatus,
  BackendType,
  ParamSpec,
  PluginDescriptor,
} from '../../types/ghostdrive';
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

type LocalZoneForm = z.infer<typeof localZoneSchema>;

// ── Types ─────────────────────────────────────────────────────────────────────

type TestState =
  | { status: 'idle' }
  | { status: 'testing' }
  | { status: 'ok'; result: BackendStatus }
  | { status: 'fail'; message: string };

interface SyncPointFormProps {
  onSuccess:        (config: BackendConfig) => void;
  onCancel:         () => void;
  /** Noms existants pour la validation d'unicité (insensible à la casse) */
  existingNames?:   string[];
  /** Warning non bloquant affiché si le rootPath remote est déjà utilisé */
  warningMessage?:  string;
  /** Config initiale — si fournie, le formulaire s'ouvre en mode édition */
  initialConfig?:   BackendConfig;
}

// ── DynamicParamField ─────────────────────────────────────────────────────────

interface DynamicParamFieldProps {
  spec:      ParamSpec;
  value:     string;
  onChange:  (v: string) => void;
  onBrowse?: () => void;
}

function DynamicParamField({ spec, value, onChange, onBrowse }: DynamicParamFieldProps) {
  const inputId = `param-${spec.key}`;

  // ── bool ──
  if (spec.type === 'bool') {
    return (
      <div className="flex items-start gap-2">
        <input
          type="checkbox"
          id={inputId}
          checked={value === 'true'}
          onChange={e => onChange(e.target.checked ? 'true' : 'false')}
          className="accent-brand mt-0.5"
        />
        <div className="flex flex-col gap-0.5">
          <label htmlFor={inputId} className="text-xs font-medium text-gray-700 cursor-pointer">
            {spec.label}
          </label>
          {spec.helpText && (
            <p className="text-xs text-gray-400">{spec.helpText}</p>
          )}
        </div>
      </div>
    );
  }

  // ── select ──
  if (spec.type === 'select') {
    return (
      <div className="flex flex-col gap-1">
        <label htmlFor={inputId} className="text-xs font-medium text-gray-700">
          {spec.label}
          {spec.required && <span className="text-red-500 ml-0.5">*</span>}
        </label>
        <select
          id={inputId}
          value={value}
          onChange={e => onChange(e.target.value)}
          aria-required={spec.required}
          className="w-full rounded border border-surface-border px-3 py-1.5 text-sm bg-white focus:outline-none focus:ring-2 focus:ring-brand focus:border-transparent"
        >
          {spec.options.map(opt => (
            <option key={opt} value={opt}>{opt}</option>
          ))}
        </select>
        {spec.helpText && (
          <p className="text-xs text-gray-400">{spec.helpText}</p>
        )}
      </div>
    );
  }

  // ── path — input + bouton Parcourir ──
  if (spec.type === 'path') {
    return (
      <div className="flex flex-col gap-1">
        <label htmlFor={inputId} className="text-xs font-medium text-gray-700">
          {spec.label}
          {spec.required && <span className="text-red-500 ml-0.5">*</span>}
        </label>
        <div className="flex gap-2">
          <input
            id={inputId}
            type="text"
            value={value}
            onChange={e => onChange(e.target.value)}
            placeholder={spec.placeholder || undefined}
            aria-required={spec.required}
            className="flex-1 w-full rounded border border-surface-border px-3 py-1.5 text-sm bg-white focus:outline-none focus:ring-2 focus:ring-brand focus:border-transparent"
          />
          {onBrowse && (
            <Button
              type="button"
              variant="secondary"
              onClick={() => void onBrowse()}
              className="shrink-0"
            >
              Parcourir…
            </Button>
          )}
        </div>
        {spec.helpText && (
          <p className="text-xs text-gray-400">{spec.helpText}</p>
        )}
      </div>
    );
  }

  // ── string | password | number ──
  const inputType =
    spec.type === 'password' ? 'password' :
    spec.type === 'number'   ? 'number'   : 'text';

  return (
    <Input
      id={inputId}
      label={spec.label}
      type={inputType}
      value={value}
      onChange={e => onChange((e.target as HTMLInputElement).value)}
      placeholder={spec.placeholder || undefined}
      required={spec.required}
      hint={spec.helpText || undefined}
    />
  );
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function buildDraft(
  localZone:    LocalZoneForm,
  remoteParams: Record<string, string>,
  descriptor:   PluginDescriptor | undefined,
  backendType:  BackendType,
): Omit<BackendConfig, 'id'> {
  const localPath = localZone.localPathMode === 'manual'
    ? (localZone.manualPath ?? '')
    : '';

  // Fusionner les valeurs saisies avec les valeurs par défaut des champs non touchés
  const params: Record<string, string> = {};
  if (descriptor) {
    for (const spec of descriptor.params) {
      const val = remoteParams[spec.key] ?? spec.default ?? '';
      if (val !== '') params[spec.key] = val;
    }
  } else {
    Object.assign(params, remoteParams);
  }

  return {
    name:       localZone.name,
    type:       backendType,
    enabled:    true,
    autoSync:   false,
    localPath,
    syncDir:    localPath,
    remotePath: '/',
    params,
  };
}

// ── Component ─────────────────────────────────────────────────────────────────

export function SyncPointForm({
  onSuccess,
  onCancel,
  existingNames,
  warningMessage,
  initialConfig,
}: SyncPointFormProps) {
  const isEditMode = initialConfig !== undefined;

  const [descriptors, setDescriptors]               = useState<PluginDescriptor[]>([]);
  const [descriptorsLoading, setDescriptorsLoading] = useState(true);
  const [backendType, setBackendType]               = useState<BackendType>(
    initialConfig?.type ?? 'local',
  );
  const [remoteParams, setRemoteParams]             = useState<Record<string, string>>(
    initialConfig?.params ?? {},
  );
  const [testState, setTestState]                   = useState<TestState>({ status: 'idle' });
  const [submitting, setSubmitting]                 = useState(false);
  const [submitError, setSubmitError]               = useState<string | null>(null);
  const [ghostDriveRoot, setGhostDriveRoot]         = useState(GHOST_DRIVE_ROOT);

  // Descripteur du plugin actif
  const currentDescriptor = useMemo(
    () => descriptors.find(d => d.type === backendType),
    [descriptors, backendType],
  );

  // Chargement des descripteurs au montage
  useEffect(() => {
    ghostdriveApi.getPluginDescriptors()
      .then(ds => {
        const list = ds ?? [];
        setDescriptors(list);
        // En mode édition, le type est fixé — on ne l'écrase pas
        if (!isEditMode && list.length > 0) setBackendType(list[0].type as BackendType);
      })
      .catch(() => setDescriptors([]))
      .finally(() => setDescriptorsLoading(false));
    // isEditMode est stable (valeur à la création du composant)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    GetGhostDriveRoot()
      .then(r => { if (r) setGhostDriveRoot(r); })
      .catch(() => { /* conserve la valeur par défaut */ });
  }, []);

  // Zone LOCAL (partagée)
  // En mode édition, déduire localPathMode :
  // Si localPath se termine par /<name> ou \<name> → mode Auto probable
  // Sinon → mode Manual
  const inferredMode: 'auto' | 'manual' = initialConfig
    ? (initialConfig.localPath?.endsWith('/' + initialConfig.name) ||
       initialConfig.localPath?.endsWith('\\' + initialConfig.name))
      ? 'auto'
      : 'manual'
    : 'auto';

  const localZoneForm = useForm<LocalZoneForm>({
    resolver: zodResolver(localZoneSchema),
    defaultValues: initialConfig
      ? {
          name:          initialConfig.name,
          localPathMode: inferredMode,
          manualPath:    inferredMode === 'manual' ? (initialConfig.localPath || undefined) : undefined,
        }
      : { localPathMode: 'auto' },
  });

  // Surveillances temps réel
  const nameValue     = localZoneForm.watch('name') ?? '';
  const localPathMode = localZoneForm.watch('localPathMode') ?? 'auto';

  // Aperçus de chemin
  const safeName    = nameValue.trim() || '<nom>';
  const ghdPreview  = `GhD:\\${safeName}\\`;
  const autoPreview = `${ghostDriveRoot}\\${safeName}\\`;

  // Liste noms en minuscule pour la validation unicité
  // En mode édition, on exclut le nom du backend courant pour pouvoir le conserver
  const existingNamesLower = useMemo(
    () => (existingNames ?? [])
      .map(n => n.toLowerCase())
      .filter(n => initialConfig ? n !== initialConfig.name.toLowerCase() : true),
    [existingNames, initialConfig],
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
    setRemoteParams({});
    setTestState({ status: 'idle' });
    setSubmitError(null);
  };

  /** Valide les deux zones et retourne les données, ou null si invalide */
  const validateAndGetData = async (): Promise<{
    localZone:    LocalZoneForm;
    remoteParams: Record<string, string>;
  } | null> => {
    const localValid = await localZoneForm.trigger();

    // Unicité du nom (vérification après zod)
    const name = localZoneForm.getValues('name');
    if (name && existingNamesLower.includes(name.toLowerCase())) {
      localZoneForm.setError('name', { message: 'Un backend avec ce nom existe déjà' });
      return null;
    }
    if (!localValid) return null;

    // Validation manuelle des champs Remote requis
    if (!currentDescriptor) {
      setSubmitError('Aucun plugin disponible');
      return null;
    }
    for (const spec of currentDescriptor.params) {
      const val = remoteParams[spec.key] ?? spec.default ?? '';
      if (spec.required && !val.trim()) {
        setSubmitError(`Le champ "${spec.label}" est requis`);
        return null;
      }
    }
    setSubmitError(null);
    return { localZone: localZoneForm.getValues(), remoteParams };
  };

  const handleTest = async () => {
    const data = await validateAndGetData();
    if (!data) return;
    setTestState({ status: 'testing' });
    try {
      const result = await ghostdriveApi.testBackendConnection(
        {
          ...buildDraft(data.localZone, data.remoteParams, currentDescriptor, backendType),
          id: '',
        } as BackendConfig,
      );
      setTestState({ status: 'ok', result });
    } catch (e) {
      setTestState({ status: 'fail', message: (e as Error).message ?? 'Connexion impossible' });
    }
  };

  const handleSave = async () => {
    const data = await validateAndGetData();
    if (!data) return;
    setSubmitError(null);
    setSubmitting(true);
    try {
      const draft = buildDraft(data.localZone, data.remoteParams, currentDescriptor, backendType);
      if (isEditMode && initialConfig) {
        // Mode édition — préserver enabled/autoSync, passer l'id existant
        const updated = await ghostdriveApi.updateBackend({
          ...draft,
          id:       initialConfig.id,
          enabled:  initialConfig.enabled,
          autoSync: initialConfig.autoSync,
        } as BackendConfig);
        onSuccess(updated);
      } else {
        const created = await ghostdriveApi.addBackend(
          { ...draft, id: '' } as BackendConfig,
        );
        onSuccess(created);
      }
    } catch (e) {
      setSubmitError(
        (e as Error).message ??
        (isEditMode ? 'Erreur lors de la modification.' : "Erreur lors de l'ajout."),
      );
    } finally {
      setSubmitting(false);
    }
  };

  // Sélecteur de dossier natif — Zone LOCAL
  const handleBrowseManualPath = async () => {
    const dir = await SelectDirectory();
    if (dir) localZoneForm.setValue('manualPath', dir, { shouldValidate: true });
  };

  // Sélecteur de dossier natif — Zone REMOTE (params de type 'path')
  const handleBrowseParam = (key: string) => async () => {
    const dir = await SelectDirectory();
    if (dir) setRemoteParams(prev => ({ ...prev, [key]: dir }));
  };

  // ── Render ────────────────────────────────────────────────────────────────

  if (descriptorsLoading) {
    return <p className="text-sm text-gray-400 text-center py-6">Chargement des plugins...</p>;
  }

  if (descriptors.length === 0) {
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

      {/* Sélecteur de type — désactivé en mode édition */}
      <div>
        <p className="text-xs font-medium text-gray-700 mb-1.5">Type de backend</p>
        <div className="flex gap-2" role="group" aria-label="Type de backend">
          {descriptors.map(d => (
            <button
              key={d.type}
              type="button"
              onClick={() => !isEditMode && switchType(d.type as BackendType)}
              disabled={isEditMode}
              className={`flex-1 py-1.5 text-sm rounded border transition-colors
                ${backendType === d.type
                  ? 'border-brand bg-brand-light text-brand font-medium'
                  : 'border-surface-border text-gray-600 hover:bg-surface-secondary'
                }
                ${isEditMode ? 'cursor-not-allowed opacity-70' : ''}
              `}
              aria-pressed={backendType === d.type}
            >
              {d.displayName}
            </button>
          ))}
        </div>
        {isEditMode && (
          <p className="text-xs text-gray-400 mt-1">
            Le type de backend ne peut pas être modifié.
          </p>
        )}
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
            Remote — {currentDescriptor?.displayName ?? backendType}
          </span>
          <div className="h-px flex-1 bg-gray-200" aria-hidden="true" />
        </div>

        <div className="flex flex-col gap-3">
          {currentDescriptor?.params.map(spec => (
            <DynamicParamField
              key={spec.key}
              spec={spec}
              value={remoteParams[spec.key] ?? spec.default ?? ''}
              onChange={v => setRemoteParams(prev => ({ ...prev, [spec.key]: v }))}
              onBrowse={spec.type === 'path' ? handleBrowseParam(spec.key) : undefined}
            />
          ))}
          {!currentDescriptor && (
            <p className="text-xs text-gray-400 italic">
              Aucun paramètre pour ce type de backend.
            </p>
          )}
        </div>
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
          onClick={() => void handleSave()}
        >
          {submitting
            ? (isEditMode ? 'Enregistrement...' : 'Ajout...')
            : (isEditMode ? 'Enregistrer' : 'Ajouter')}
        </Button>
      </div>
    </div>
  );
}
