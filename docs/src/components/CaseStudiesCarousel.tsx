'use client'

import { useState } from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { caseStudies } from "@/constants/caseStudies";

export const CaseStudiesCarousel = () => {
  const [currentIndex, setCurrentIndex] = useState(0);

  const next = () => setCurrentIndex((i) => (i + 1) % caseStudies.length);
  const prev = () => setCurrentIndex((i) => (i - 1 + caseStudies.length) % caseStudies.length);

  const study = caseStudies[currentIndex];

  return (
    <section className="relative py-32 bg-secondary/30">
      <div className="container mx-auto px-4 sm:px-6 lg:px-8">
        <div className="text-center max-w-3xl mx-auto mb-20">
          <h2 className="text-5xl sm:text-6xl font-black mb-6">
            Success stories.
          </h2>
          <p className="text-xl text-muted-foreground">
            See how teams are building amazing products with Forge.
          </p>
        </div>

        <div className="max-w-5xl mx-auto">
          <div className={`rounded-3xl border border-border bg-gradient-to-br ${study.gradient} p-1`}>
            <div className="rounded-[calc(1.5rem-1px)] bg-card p-12">
              <div className="flex items-center gap-4 mb-8">
                <div className={`w-16 h-16 rounded-2xl bg-gradient-to-br ${study.gradient} flex items-center justify-center text-white font-black text-2xl`}>
                  {study.logo}
                </div>
                <div>
                  <div className="text-2xl font-black">{study.company}</div>
                  <div className="text-sm text-muted-foreground">{study.industry}</div>
                </div>
              </div>

              <h3 className="text-4xl font-black mb-6">{study.title}</h3>
              <p className="text-xl text-muted-foreground mb-12 leading-relaxed">
                {study.description}
              </p>

              <div className="grid sm:grid-cols-3 gap-8 mb-12">
                {study.metrics.map((metric, i) => (
                  <div key={i} className="text-center">
                    <div className={`text-4xl font-black bg-gradient-to-r ${study.gradient} bg-clip-text text-transparent mb-2`}>
                      {metric.value}
                    </div>
                    <div className="text-sm text-muted-foreground">{metric.label}</div>
                  </div>
                ))}
              </div>

              <div className="flex justify-center gap-4">
                <Button variant="outline" size="icon" onClick={prev}>
                  <ChevronLeft className="w-5 h-5" />
                </Button>
                <div className="flex items-center gap-2">
                  {caseStudies.map((_, i) => (
                    <button
                      key={i}
                      onClick={() => setCurrentIndex(i)}
                      className={`w-2 h-2 rounded-full transition-all ${
                        i === currentIndex ? "bg-primary w-8" : "bg-border"
                      }`}
                    />
                  ))}
                </div>
                <Button variant="outline" size="icon" onClick={next}>
                  <ChevronRight className="w-5 h-5" />
                </Button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
};
