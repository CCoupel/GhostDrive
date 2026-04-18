import type { ButtonHTMLAttributes, ReactNode } from 'react';

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger';
  size?: 'sm' | 'md';
  children: ReactNode;
}

const variantClasses = {
  primary:   'bg-brand text-white hover:bg-brand-hover active:brightness-90',
  secondary: 'bg-surface-secondary text-gray-900 border border-surface-border hover:bg-gray-100',
  ghost:     'text-gray-700 hover:bg-surface-secondary',
  danger:    'bg-red-600 text-white hover:bg-red-700',
};

const sizeClasses = {
  sm: 'px-2.5 py-1 text-xs',
  md: 'px-3.5 py-1.5 text-sm',
};

export function Button({
  variant = 'secondary',
  size = 'md',
  className = '',
  disabled,
  children,
  ...props
}: ButtonProps) {
  return (
    <button
      {...props}
      disabled={disabled}
      className={`
        inline-flex items-center gap-1.5 rounded font-medium transition-colors
        focus-visible:ring-2 focus-visible:ring-brand focus-visible:outline-none
        disabled:opacity-50 disabled:cursor-not-allowed
        ${variantClasses[variant]} ${sizeClasses[size]} ${className}
      `.trim()}
    >
      {children}
    </button>
  );
}
