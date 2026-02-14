'use client';

import { motion, useInView, useMotionValue, useTransform, animate } from 'framer-motion';
import { useEffect, useRef } from 'react';

const stats = [
  { value: 28, suffix: '+', label: 'Extensions' },
  { value: 6, suffix: '+', label: 'Protocols' },
  { value: 100, suffix: '%', label: 'Type-Safe' },
  { value: 5, suffix: 'x', label: 'Faster Dev' },
];

function AnimatedNumber({ value, suffix }: { value: number; suffix: string }) {
  const ref = useRef<HTMLSpanElement>(null);
  const motionVal = useMotionValue(0);
  const rounded = useTransform(motionVal, (v) => Math.round(v));
  const inView = useInView(ref, { once: true, margin: '-40px' });

  useEffect(() => {
    if (inView) {
      animate(motionVal, value, { duration: 1.5, ease: 'easeOut' });
    }
  }, [inView, motionVal, value]);

  return (
    <span ref={ref} className="text-3xl md:text-4xl font-bold tabular-nums">
      <motion.span>{rounded}</motion.span>
      {suffix}
    </span>
  );
}

export function StatsBar() {
  return (
    <section className="container max-w-(--fd-layout-width) mx-auto px-6 py-16 md:py-24">
      <div className="relative overflow-hidden border border-fd-border bg-fd-card">
        <div className="absolute inset-0 bg-fd-card z-0" />
        <div className="metal-shader absolute inset-0 opacity-50 z-0 pointer-events-none" />
        <div className="noise-overlay absolute inset-0 pointer-events-none opacity-50" />

        <div className="relative z-10 grid grid-cols-2 md:grid-cols-4 divide-x divide-fd-border">
          {stats.map((stat) => (
            <motion.div
              key={stat.label}
              initial={{ opacity: 0, y: 16 }}
              whileInView={{ opacity: 1, y: 0 }}
              viewport={{ once: true, margin: '-40px' }}
              transition={{ duration: 0.5, ease: 'easeOut' }}
              className="flex flex-col items-center justify-center py-8 md:py-10"
            >
              <AnimatedNumber value={stat.value} suffix={stat.suffix} />
              <span className="text-sm text-fd-muted-foreground mt-1">{stat.label}</span>
            </motion.div>
          ))}
        </div>
      </div>
    </section>
  );
}
