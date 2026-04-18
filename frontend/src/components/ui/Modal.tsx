import type { ReactNode } from 'react';
import { useEffect } from 'react';
import { X } from 'lucide-react';
import { Button } from './Button';

interface ModalProps {
  open: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
  footer?: ReactNode;
}

export function Modal({ open, onClose, title, children, footer }: ModalProps) {
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  if (!open) return null;

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="modal-title"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/30"
      onClick={onClose}
    >
      <div
        className="bg-white rounded-lg shadow-xl w-full max-w-md mx-4 flex flex-col max-h-[90vh]"
        onClick={e => e.stopPropagation()}
      >
        <div className="flex items-center justify-between px-4 py-3 border-b border-surface-border">
          <h2 id="modal-title" className="font-semibold text-gray-900">
            {title}
          </h2>
          <Button variant="ghost" size="sm" onClick={onClose} aria-label="Fermer">
            <X size={16} />
          </Button>
        </div>

        <div className="flex-1 overflow-y-auto px-4 py-3">
          {children}
        </div>

        {footer && (
          <div className="flex justify-end gap-2 px-4 py-3 border-t border-surface-border">
            {footer}
          </div>
        )}
      </div>
    </div>
  );
}
