/**
 * Action dispatcher.
 *
 * Components emit Action objects (declared on the Go side as the Action
 * sum-type — see contract/components/actions.go). dispatchAction reads
 * action.kind and performs the side effect.
 *
 * Some kinds (RunCommand, OpenDialog) need React-runtime hooks; for now
 * these are placeholders — the renderer that produced the Action should
 * handle them inline via useContractCommand or similar. The dispatcher
 * here covers the pure side-effect kinds (Navigate, Copy, Toast, Emit).
 */

export type ActionKind =
  | "navigate"
  | "openDialog"
  | "closeDialog"
  | "runCommand"
  | "setState"
  | "emit"
  | "confirm"
  | "copy"
  | "toast";

export interface Action {
  kind: ActionKind;

  // Navigate
  href?: string;
  replace?: boolean;

  // OpenDialog / CloseDialog
  dialog?: string;

  // RunCommand
  intent?: string;
  contributor?: string;
  params?: Record<string, unknown>;

  // SetState
  stateKey?: string;
  stateValue?: unknown;

  // Emit
  event?: string;
  payload?: Record<string, unknown>;

  // Confirm — wraps another Action
  confirmMessage?: string;
  confirmTitle?: string;
  confirmAction?: Action;

  // Copy
  copyValue?: string;

  // Toast
  toastMessage?: string;
  toastVariant?: "default" | "destructive";
}

export interface ActionContext {
  parent?: unknown;
  session?: unknown;
  route?: Record<string, string>;
  contributor?: string | null;
  value?: unknown; // current control value (input onChange et al.)
  navigate?: (href: string, opts?: { replace?: boolean }) => void;
}

/**
 * dispatchAction performs the side effect for an Action. Returns true
 * when the action was handled; false when no handler is wired (callers
 * can fall back to logging or own handling).
 */
export function dispatchAction(action: Action | undefined, ctx: ActionContext = {}): boolean {
  if (!action) return false;
  switch (action.kind) {
    case "navigate":
      if (!action.href) return false;
      if (ctx.navigate) {
        ctx.navigate(action.href, { replace: action.replace });
      } else if (typeof window !== "undefined") {
        // Hard fallback when no router context is available.
        if (action.replace) window.location.replace(action.href);
        else window.location.assign(action.href);
      }
      return true;
    case "copy":
      if (typeof navigator !== "undefined" && navigator.clipboard && action.copyValue) {
        void navigator.clipboard.writeText(action.copyValue);
        return true;
      }
      return false;
    case "emit":
      if (action.event && typeof window !== "undefined") {
        window.dispatchEvent(
          new CustomEvent(action.event, { detail: action.payload }),
        );
        return true;
      }
      return false;
    case "toast":
      // No global toast bus yet — log so the path is at least observable.
      console.info("[toast]", action.toastVariant ?? "default", action.toastMessage);
      return true;
    case "setState":
    case "openDialog":
    case "closeDialog":
    case "runCommand":
    case "confirm":
      // These require renderer-level state or React hooks; the producing
      // renderer handles them inline. The dispatcher is a no-op for now
      // and returns false so callers can take their own path.
      return false;
    default:
      return false;
  }
}

/** Toast builds a default-variant toast Action. Symmetric to the Go
 * components.Toast() helper. */
export function Toast(message: string): Action {
  return { kind: "toast", toastMessage: message, toastVariant: "default" };
}

/** ToastError builds a destructive-variant toast Action. */
export function ToastError(message: string): Action {
  return { kind: "toast", toastMessage: message, toastVariant: "destructive" };
}
