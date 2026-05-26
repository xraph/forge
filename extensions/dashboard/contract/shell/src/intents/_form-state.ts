import * as React from "react";
import { FormStateContext } from "./form.edit";

/**
 * useFormStateOptional returns the current FormState if the caller is
 * inside a form.edit (or any future organism.dynamic-form) context.
 * Returns null when used standalone — atoms work both inside and
 * outside forms.
 */
export function useFormStateOptional() {
  return React.useContext(FormStateContext);
}
