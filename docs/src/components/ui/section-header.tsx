interface SectionHeaderProps {
  label?: string;
  title: string;
  description: string;
  centered?: boolean;
}

export const SectionHeader = ({ 
  label, 
  title, 
  description, 
  centered = true 
}: SectionHeaderProps) => {
  return (
    <div className={`max-w-3xl ${centered ? 'mx-auto text-center' : ''} mb-20`}>
      {label && (
        <div className="text-sm font-semibold tracking-wide text-muted-foreground mb-4 uppercase">
          {label}
        </div>
      )}
      <h2 className="text-5xl sm:text-6xl font-black mb-6">
        {title}
      </h2>
      <p className="text-xl text-muted-foreground">
        {description}
      </p>
    </div>
  );
};
