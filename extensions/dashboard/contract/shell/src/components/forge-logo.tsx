import * as React from "react";

interface ForgeLogoProps extends React.SVGProps<SVGSVGElement> {
  /** Tailwind size class; defaults to h-5 w-5 to match a lucide icon slot. */
  className?: string;
}

/**
 * ForgeLogo renders the canonical forge mark (mirrors assets/logo.svg in the
 * repo root, also shipped by the docs site). Inlined as a React component
 * rather than imported as an asset so fills can use `currentColor` — this
 * lets the logo automatically follow the sidebar's foreground token in
 * both light and dark mode without shipping two variants.
 */
export function ForgeLogo({ className, ...rest }: ForgeLogoProps) {
  return (
    <svg
      viewBox="0 0 559 552"
      fill="currentColor"
      xmlns="http://www.w3.org/2000/svg"
      className={className ?? "h-5 w-5"}
      aria-hidden="true"
      {...rest}
    >
      <rect x="559" y="0" width="125" height="425" transform="rotate(90 559 0)" />
      <rect x="425" y="218" width="125" height="291" transform="rotate(90 425 218)" />
      <path d="M432 342.304V218H558.551L432 342.304Z" />
      <path d="M0 551.304V136H127V427L0 551.304Z" />
      <path d="M127 125H0.342773L127 0.137726V125Z" />
    </svg>
  );
}
