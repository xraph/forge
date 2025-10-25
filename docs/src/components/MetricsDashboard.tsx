'use client'

import { useEffect, useState } from "react";
import { metrics } from "@/constants/metrics";

const AnimatedCounter = ({ value, suffix }: { value: number; suffix: string }) => {
  const [count, setCount] = useState(0);

  useEffect(() => {
    const duration = 2000;
    const steps = 60;
    const increment = value / steps;
    let current = 0;

    const timer = setInterval(() => {
      current += increment;
      if (current >= value) {
        setCount(value);
        clearInterval(timer);
      } else {
        setCount(Math.floor(current));
      }
    }, duration / steps);

    return () => clearInterval(timer);
  }, [value]);

  return (
    <span className="text-5xl sm:text-6xl font-black bg-gradient-to-r from-primary via-accent to-primary bg-clip-text text-transparent">
      {count.toLocaleString()}{suffix}
    </span>
  );
};

export const MetricsDashboard = () => {
  return (
    <section className="relative py-32 overflow-hidden">
      <div className="absolute inset-0 gradient-mesh opacity-20" />
      
      <div className="container relative mx-auto px-4 sm:px-6 lg:px-8">
        <div className="text-center max-w-3xl mx-auto mb-20">
          <h2 className="text-5xl sm:text-6xl font-black mb-6">
            Proven at scale.
          </h2>
          <p className="text-xl text-muted-foreground">
            Join thousands of developers building the next generation of backends.
          </p>
        </div>

        <div className="grid sm:grid-cols-2 lg:grid-cols-4 gap-8">
          {metrics.map((metric, index) => (
            <div
              key={index}
              className="text-center p-8 rounded-3xl border border-border bg-card hover-lift"
            >
              <AnimatedCounter value={metric.value} suffix={metric.suffix} />
              <div className="mt-4 text-xl font-bold">{metric.label}</div>
              <div className="mt-2 text-sm text-muted-foreground">
                {metric.description}
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
};
