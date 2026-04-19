import type { ProgressEvent } from '../../types/ghostdrive';
import { formatBytes } from '../../utils/formatBytes';

interface ProgressBarProps {
  value: number;
  max?: number;
  label?: string;
  showPercent?: boolean;
  className?: string;
}

export function ProgressBar({
  value,
  max = 100,
  label,
  showPercent = false,
  className = '',
}: ProgressBarProps) {
  const percent = Math.min(100, Math.max(0, (value / max) * 100));

  return (
    <div className={`w-full ${className}`}>
      {(label || showPercent) && (
        <div className="flex justify-between text-xs text-gray-500 mb-1">
          {label && <span className="truncate">{label}</span>}
          {showPercent && <span className="shrink-0 ml-2">{Math.round(percent)}%</span>}
        </div>
      )}
      <div
        role="progressbar"
        aria-valuenow={percent}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-label={label}
        className="h-1.5 w-full rounded-full bg-surface-border overflow-hidden"
      >
        <div
          className="h-full bg-brand rounded-full transition-all duration-300 ease-out"
          style={{ width: `${percent}%` }}
        />
      </div>
    </div>
  );
}


interface TransferProgressBarProps {
  transfer: ProgressEvent;
  className?: string;
}

export function TransferProgressBar({ transfer, className = '' }: TransferProgressBarProps) {
  const { path, direction, bytesDone, bytesTotal, percent } = transfer;
  const filename = path.split(/[/\\]/).pop() ?? path;
  const arrow    = direction === 'upload' ? '↑' : '↓';

  return (
    <div className={`w-full ${className}`}>
      <div className="flex justify-between text-xs text-gray-500 mb-1">
        <span className="truncate">
          <span className={direction === 'upload' ? 'text-brand' : 'text-status-syncing'}>
            {arrow}
          </span>
          {' '}
          <span title={path}>{filename}</span>
        </span>
        <span className="shrink-0 ml-2 tabular-nums">
          {formatBytes(bytesDone)} / {formatBytes(bytesTotal)}
        </span>
      </div>
      <div
        role="progressbar"
        aria-valuenow={percent}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-label={`${arrow} ${filename}`}
        className="h-1.5 w-full rounded-full bg-surface-border overflow-hidden"
      >
        <div
          className="h-full bg-brand rounded-full transition-all duration-300 ease-out"
          style={{ width: `${Math.min(100, percent)}%` }}
        />
      </div>
    </div>
  );
}
