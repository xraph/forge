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
      {words.map((word, wi) => (
        <span
          key={wi}
          className={
            wi === 0
              ? "text-transparent bg-clip-text bg-gradient-to-r from-amber-400 via-orange-500 to-red-500"
              : ""
          }
        >
          {word.split("").map((char) => {
            const idx = charIndex++;
            return (
              <motion.span
                key={idx}
                custom={idx}
                variants={charVariants}
                initial="hidden"
                animate={inView ? "visible" : "hidden"}
                className="inline-block"
                style={char === " " ? { width: "0.3em" } : undefined}
              >
                {char === " " ? "\u00A0" : char}
              </motion.span>
            );
          })}
        </span>
      ))}
    </h1>
  );
}
