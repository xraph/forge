interface NoiseBackgroundProps {
  children: React.ReactNode;
  className?: string;
  gradient?: string;
}

export function NoiseBackground({
  children,
  className = '',
  gradient = 'bg-[radial-gradient(ellipse_80%_60%_at_50%_-20%,rgba(245,158,11,0.18),transparent_70%)]',
}: NoiseBackgroundProps) {
  return (
    <div className={`relative overflow-hidden ${className}`}>
      <div className={`absolute inset-0 -z-10 ${gradient}`} />
      <div className="noise-overlay absolute inset-0 -z-10" aria-hidden="true">
        <div className="absolute inset-0" />
      </div>
      {children}
    </div>
  );
}
