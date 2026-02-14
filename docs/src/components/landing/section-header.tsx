'use client';

import { cn } from '@/lib/cn';
import { motion } from 'framer-motion';

interface SectionHeaderProps {
  title: string;
  description?: string;
  badge?: string;
  className?: string;
  leftAlign?: boolean;
}

export function SectionHeader({
  title,
  description,
  badge,
  className = '',
  leftAlign = false,
}: SectionHeaderProps) {
  return (
    <motion.div
      initial={{ opacity: 0, y: 24 }}
      whileInView={{ opacity: 1, y: 0 }}
      viewport={{ once: true, margin: '-80px' }}
      transition={{ duration: 0.6, ease: 'easeOut' }}
      className={`text-center mb-14 ${className}`}
    >
      {badge && (
        <span className="inline-flex items-center gap-2 border border-amber-500/30 bg-amber-500/10 px-4 py-1.5 text-sm text-amber-600 dark:text-amber-400 mb-4">
          {badge}
        </span>
      )}
      <h2 className="text-3xl font-bold tracking-tight sm:text-4xl">{title}</h2>
      {description && (
        <p className={cn("mt-4 max-w-2xl text-fd-muted-foreground text-lg leading-relaxed", leftAlign ? "text-left" : "mx-auto text-center")}>
          {description}
        </p>
      )}
    </motion.div>
  );
}
