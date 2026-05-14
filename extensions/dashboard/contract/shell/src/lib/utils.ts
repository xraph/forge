import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

/** cn merges Tailwind class strings, deduplicating conflicts via tailwind-merge. */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
