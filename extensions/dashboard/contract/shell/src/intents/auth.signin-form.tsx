import { AuthLoginForm } from "../auth/AuthLoginForm";
import type { IntentComponentProps } from "../runtime/registry";

interface SignInFormProps {
  op?: string;
  configContributor?: string;
  showRememberMe?: boolean;
  showMagicLink?: boolean;
  showSignUpLink?: boolean;
  signUpURL?: string;
  forgotPasswordURL?: string;
  brand?: string;
  brandLogoURL?: string;
}

/**
 * auth.signin-form delegates to the existing AuthLoginForm renderer
 * (which already handles auth.config fetch, OAuth providers, and password
 * submit). Phase 4 extensions (remember-me, magic-link toggle) flow
 * through as props; the underlying LoginCard surfaces them when the
 * auth.config response declares them.
 */
export function SignInForm(props: IntentComponentProps<unknown, SignInFormProps>) {
  return (
    <AuthLoginForm
      {...props}
      props={{
        op: props.props.op,
        configContributor: props.props.configContributor,
      }}
    />
  );
}
