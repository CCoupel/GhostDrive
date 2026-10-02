/**
 * Toast — notification non-bloquante (v2.2 #144 Avertissement Conflict)
 *
 * Usage :
 *   <Toast message="Conflit détecté : foo.docx" type="conflict" onDismiss={fn} />
 *
 * Comportement :
 *   - Auto-dismiss après 5 s
 *   - Clic sur le toast → dismiss immédiat
 *   - Bouton × → dismiss (sans propager le clic parent)
 *   - Accessible : role="alert" + aria-live="polite"
 */

import { useEffect, useRef } from 'react';
import { AlertTriangle, AlertCircle, Info, X } from 'lucide-react';

// ── Types ─────────────────────────────────────────────────────────────────────

export type ToastType = 'conflict' | 'error' | 'info';

export interface ToastProps {
  message:    string;
  type:       ToastType;
  onDismiss?: () => void;
}

// ── Constantes ────────────────────────────────────────────────────────────────

const AUTO_DISMISS_MS = 5_000;

const ICONS: Record<ToastType, JSX.Element> = {
  conflict: <AlertTriangle size={16} color="#f59e0b" aria-hidden="true" />,
  error:    <AlertCircle  size={16} color="#ef4444" aria-hidden="true" />,
  info:     <Info         size={16} color="#3b82f6" aria-hidden="true" />,
};

const BG_CLASSES: Record<ToastType, string> = {
  conflict: 'border-amber-300  bg-amber-50',
  error:    'border-red-300    bg-red-50',
  info:     'border-blue-300   bg-blue-50',
};

// ── Composant ─────────────────────────────────────────────────────────────────

export function Toast({ message, type, onDismiss }: ToastProps) {
  // Stocker onDismiss dans un ref pour que le timer ne capte jamais une closure
  // périmée même si la callback change entre deux rendus.
  const onDismissRef = useRef(onDismiss);
  useEffect(() => { onDismissRef.current = onDismiss; }, [onDismiss]);

  // Auto-dismiss — déclenché une seule fois au mount
  useEffect(() => {
    const timer = setTimeout(() => { onDismissRef.current?.(); }, AUTO_DISMISS_MS);
    return () => clearTimeout(timer);
  }, []);

  const handleCardClick = () => onDismissRef.current?.();

  const handleXClick = (e: React.MouseEvent) => {
    e.stopPropagation(); // éviter le double appel avec handleCardClick
    onDismissRef.current?.();
  };

  return (
    <div
      role="alert"
      aria-live="polite"
      aria-atomic="true"
      className={`
        flex items-start gap-2 px-3 py-2.5 rounded-md border shadow-md
        text-sm max-w-xs w-72 cursor-pointer select-none
        transition-opacity duration-200
        ${BG_CLASSES[type]}
      `}
      onClick={handleCardClick}
      aria-label={`Notification : ${message}`}
    >
      {/* Icône */}
      <span className="mt-0.5 shrink-0">
        {ICONS[type]}
      </span>

      {/* Message */}
      <span className="flex-1 text-gray-800 leading-snug break-words">
        {message}
      </span>

      {/* Bouton fermer */}
      <button
        type="button"
        onClick={handleXClick}
        className="shrink-0 mt-0.5 text-gray-400 hover:text-gray-700 transition-colors rounded focus-visible:ring-1 focus-visible:ring-brand focus-visible:outline-none"
        aria-label="Fermer la notification"
      >
        <X size={14} />
      </button>
    </div>
  );
}
