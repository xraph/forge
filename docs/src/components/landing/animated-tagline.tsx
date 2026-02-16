"use client";

import { motion, useInView } from "framer-motion";
import { useRef } from "react";

const words = ["Forge", " your", " backend"];

const charVariants = {
  hidden: { opacity: 0, y: 10, filter: "blur(4px)" },
  visible: (i: number) => ({
    opacity: 1,
    y: 0,
    filter: "blur(0px)",
    transition: {
      delay: i * 0.035,
      duration: 0.4,
      ease: [0.25, 0.46, 0.45, 0.94] as const,
    },
  }),
};

export function AnimatedTagline() {
  const ref = useRef<HTMLHeadingElement>(null);
  const inView = useInView(ref, { once: true, margin: "-40px" });

  let charIndex = 0;

  return (
    <h1
      ref={ref}
      aria-label="Forge your backend"
      className="text-4xl font-bold tracking-tight sm:text-5xl lg:text-6xl leading-[1.1]"
    >
      {words.map((word, wi) => {
        const chars = word.split("");
        return (
          <span key={wi}>
            {chars.map((char, ci) => {
              const idx = charIndex++;
              const isForge = wi === 0;
              return (
                <motion.span
                  key={idx}
                  custom={idx}
                  variants={charVariants}
                  initial="hidden"
                  animate={inView ? "visible" : "hidden"}
                  className={`inline-block ${isForge ? "text-transparent bg-clip-text" : ""}`}
                  style={
                    isForge
                      ? {
                          backgroundImage:
                            "linear-gradient(to right, #fbbf24, #f97316, #ef4444)",
                          backgroundSize: `${chars.length * 100}% 100%`,
                          backgroundPosition: `${
                            chars.length > 1
                              ? (ci / (chars.length - 1)) * 100
                              : 0
                          }% 0`,
                        }
                      : char === " "
                        ? { width: "0.3em" }
                        : undefined
                  }
                >
                  {char === " " ? "\u00A0" : char}
                </motion.span>
              );
            })}
          </span>
        );
      })}
    </h1>
  );
}
