import * as React from "react";
import { GalleryVerticalEnd, AlertCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldSeparator,
} from "@/components/ui/field";
import { cn } from "@/lib/utils";

// Slice (l.5) — visual layer for the dashboard login page. Mirrors
// shadcn's login-04 block: brand lockup, "Welcome to {brand}", optional
// signup link, email + submit, "Or" separator, social provider buttons,
// terms/privacy footer.
//
// Both the contract-driven AuthLoginForm and the built-in LoginScreen
// fallback render this surface so the visual is identical regardless of
// who's contributing the form. Provider buttons are data-driven via
// `socialProviders`; absence collapses the section gracefully.

export interface SocialProvider {
  id: string;
  label: string;
  /** Click handler — implementations either redirect to the upstream OAuth
   *  start endpoint (contract path) or throw "not configured" (fallback). */
  onClick: () => void;
}

export interface LoginCardProps {
  brand?: string;
  brandLogoURL?: string | null;
  /** Optional sub-heading copy; defaults to a short stock line. */
  description?: React.ReactNode;
  /** "Don't have an account? <a>Sign up</a>" — pass null to hide. */
  signupHref?: string | null;
  signupLabel?: string;
  termsURL?: string | null;
  privacyURL?: string | null;
  /** When false, hides the email/password block (e.g. social-only deployment). */
  passwordEnabled?: boolean;
  socialProviders?: SocialProvider[];
  errorMsg?: string | null;
  submitting?: boolean;
  /** Submit handler invoked with the form's email + password. */
  onSubmit: (input: { email: string; password: string }) => void | Promise<void>;
  className?: string;
}

export function LoginCard({
  brand = "Forge Dashboard",
  brandLogoURL,
  description,
  signupHref,
  signupLabel = "Sign up",
  termsURL,
  privacyURL,
  passwordEnabled = true,
  socialProviders = [],
  errorMsg,
  submitting,
  onSubmit,
  className,
}: LoginCardProps) {
  const [email, setEmail] = React.useState("");
  const [password, setPassword] = React.useState("");
  const showPasswordBlock = passwordEnabled;
  const showSocialBlock = socialProviders.length > 0;
  const showSeparator = showPasswordBlock && showSocialBlock;
  const showFooterLegal = Boolean(termsURL || privacyURL);

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    void onSubmit({ email, password });
  };

  return (
    <div className={cn("flex min-h-svh w-full items-center justify-center bg-background p-6 md:p-10", className)}>
      <div className="flex w-full max-w-sm flex-col gap-6">
        <form onSubmit={submit} noValidate>
          <FieldGroup>
            <BrandLockup brand={brand} logoURL={brandLogoURL} signupHref={signupHref} signupLabel={signupLabel} />
            {showPasswordBlock ? (
              <>
                <Field>
                  <FieldLabel htmlFor="login-email">Email</FieldLabel>
                  <Input
                    id="login-email"
                    type="email"
                    placeholder="m@example.com"
                    autoComplete="email"
                    required
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                  />
                </Field>
                <Field>
                  <FieldLabel htmlFor="login-password">Password</FieldLabel>
                  <Input
                    id="login-password"
                    type="password"
                    autoComplete="current-password"
                    required
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                  />
                </Field>
                {errorMsg ? (
                  <div
                    role="alert"
                    className="flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive"
                  >
                    <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />
                    <span className="leading-tight">{errorMsg}</span>
                  </div>
                ) : null}
                <Field>
                  <Button type="submit" disabled={submitting}>
                    {submitting ? "Signing in…" : "Login"}
                  </Button>
                </Field>
              </>
            ) : null}
            {showSeparator ? <FieldSeparator>Or</FieldSeparator> : null}
            {showSocialBlock ? (
              <Field className={cn("grid gap-3", socialProviders.length > 1 && "sm:grid-cols-2")}>
                {socialProviders.map((p) => (
                  <Button
                    key={p.id}
                    variant="outline"
                    type="button"
                    onClick={p.onClick}
                    disabled={submitting}
                    className="gap-2"
                  >
                    <ProviderIcon provider={p.id} />
                    {p.label}
                  </Button>
                ))}
              </Field>
            ) : null}
            {description ? (
              <FieldDescription className="text-center">{description}</FieldDescription>
            ) : null}
          </FieldGroup>
        </form>
        {showFooterLegal ? (
          <FieldDescription className="px-6 text-center">
            By clicking continue, you agree to our{" "}
            {termsURL ? <a href={termsURL}>Terms of Service</a> : <span>Terms of Service</span>}
            {" "}and{" "}
            {privacyURL ? <a href={privacyURL}>Privacy Policy</a> : <span>Privacy Policy</span>}.
          </FieldDescription>
        ) : null}
      </div>
    </div>
  );
}

function BrandLockup({
  brand,
  logoURL,
  signupHref,
  signupLabel,
}: {
  brand: string;
  logoURL?: string | null;
  signupHref?: string | null;
  signupLabel: string;
}) {
  return (
    <div className="flex flex-col items-center gap-2 text-center">
      <a href="#" className="flex flex-col items-center gap-2 font-medium" tabIndex={-1}>
        {logoURL ? (
          <img src={logoURL} alt={brand} className="size-8 rounded-md object-contain" />
        ) : (
          <div className="flex size-8 items-center justify-center rounded-md">
            <GalleryVerticalEnd className="size-6" aria-hidden />
          </div>
        )}
        <span className="sr-only">{brand}</span>
      </a>
      <h1 className="text-xl font-bold">Welcome to {brand}</h1>
      {signupHref ? (
        <FieldDescription>
          Don&apos;t have an account? <a href={signupHref}>{signupLabel}</a>
        </FieldDescription>
      ) : null}
    </div>
  );
}

// ProviderIcon resolves a provider id to its brand glyph. Inline SVGs match
// shadcn's login-04 reference; unknown providers fall back to a generic
// circle. Adding a new provider is a one-liner here.
function ProviderIcon({ provider }: { provider: string }) {
  switch (provider) {
    case "apple":
      return (
        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" className="size-4" aria-hidden>
          <path
            d="M12.152 6.896c-.948 0-2.415-1.078-3.96-1.04-2.04.027-3.91 1.183-4.961 3.014-2.117 3.675-.546 9.103 1.519 12.09 1.013 1.454 2.208 3.09 3.792 3.039 1.52-.065 2.09-.987 3.935-.987 1.831 0 2.35.987 3.96.948 1.637-.026 2.676-1.48 3.676-2.948 1.156-1.688 1.636-3.325 1.662-3.415-.039-.013-3.182-1.221-3.22-4.857-.026-3.04 2.48-4.494 2.597-4.559-1.429-2.09-3.623-2.324-4.39-2.376-2-.156-3.675 1.09-4.61 1.09zM15.53 3.83c.843-1.012 1.4-2.427 1.245-3.83-1.207.052-2.662.805-3.532 1.818-.78.896-1.454 2.338-1.273 3.714 1.338.104 2.715-.688 3.559-1.701"
            fill="currentColor"
          />
        </svg>
      );
    case "google":
      return (
        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" className="size-4" aria-hidden>
          <path
            d="M12.48 10.92v3.28h7.84c-.24 1.84-.853 3.187-1.787 4.133-1.147 1.147-2.933 2.4-6.053 2.4-4.827 0-8.6-3.893-8.6-8.72s3.773-8.72 8.6-8.72c2.6 0 4.507 1.027 5.907 2.347l2.307-2.307C18.747 1.44 16.133 0 12.48 0 5.867 0 .307 5.387.307 12s5.56 12 12.173 12c3.573 0 6.267-1.173 8.373-3.36 2.16-2.16 2.84-5.213 2.84-7.667 0-.76-.053-1.467-.173-2.053H12.48z"
            fill="currentColor"
          />
        </svg>
      );
    case "github":
      return (
        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" className="size-4" aria-hidden>
          <path
            fill="currentColor"
            d="M12 .297a12 12 0 0 0-3.79 23.387c.6.111.82-.26.82-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.418-1.305.762-1.605-2.665-.305-5.467-1.334-5.467-5.93 0-1.31.469-2.381 1.236-3.221-.124-.303-.535-1.524.116-3.176 0 0 1.008-.323 3.301 1.23a11.5 11.5 0 0 1 6.003 0c2.292-1.553 3.299-1.23 3.299-1.23.653 1.652.241 2.873.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.218.694.825.576A12.001 12.001 0 0 0 12 .297"
          />
        </svg>
      );
    case "microsoft":
      return (
        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" className="size-4" aria-hidden>
          <path fill="currentColor" d="M11.4 24H0V12.6h11.4V24zM24 24H12.6V12.6H24V24zM11.4 11.4H0V0h11.4v11.4zM24 11.4H12.6V0H24v11.4z" />
        </svg>
      );
    case "facebook":
      return (
        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" className="size-4" aria-hidden>
          <path
            fill="currentColor"
            d="M24 12.073c0-6.627-5.373-12-12-12S0 5.446 0 12.073c0 5.99 4.388 10.954 10.125 11.854v-8.385H7.078v-3.47h3.047V9.43c0-3.007 1.792-4.669 4.533-4.669 1.312 0 2.686.235 2.686.235v2.953H15.83c-1.491 0-1.956.925-1.956 1.874v2.25h3.328l-.532 3.47h-2.796v8.385C19.612 23.027 24 18.062 24 12.073"
          />
        </svg>
      );
    case "discord":
      return (
        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" className="size-4" aria-hidden>
          <path
            fill="currentColor"
            d="M20.317 4.37a19.79 19.79 0 0 0-4.885-1.515.07.07 0 0 0-.073.034 13.57 13.57 0 0 0-.598 1.226 18.27 18.27 0 0 0-5.487 0 12.5 12.5 0 0 0-.61-1.226.077.077 0 0 0-.072-.034 19.74 19.74 0 0 0-4.885 1.515.07.07 0 0 0-.032.027C.533 9.046-.32 13.58.099 18.058a.083.083 0 0 0 .031.057 19.9 19.9 0 0 0 5.993 3.029.078.078 0 0 0 .084-.027 14.1 14.1 0 0 0 1.226-1.994.076.076 0 0 0-.041-.106 13.1 13.1 0 0 1-1.872-.892.077.077 0 0 1-.008-.128c.126-.094.252-.192.372-.291a.074.074 0 0 1 .077-.01c3.927 1.793 8.18 1.793 12.061 0a.074.074 0 0 1 .078.009c.12.099.246.198.373.292a.077.077 0 0 1-.006.127 12.3 12.3 0 0 1-1.873.892.077.077 0 0 0-.041.107c.36.698.772 1.362 1.225 1.993a.076.076 0 0 0 .084.028 19.84 19.84 0 0 0 6.002-3.03.077.077 0 0 0 .032-.054c.5-5.177-.838-9.674-3.549-13.66a.06.06 0 0 0-.031-.03zM8.02 15.331c-1.182 0-2.157-1.085-2.157-2.418 0-1.334.956-2.42 2.157-2.42 1.21 0 2.176 1.096 2.157 2.42 0 1.333-.956 2.418-2.157 2.418m7.974 0c-1.183 0-2.157-1.085-2.157-2.418 0-1.334.955-2.42 2.157-2.42 1.21 0 2.176 1.096 2.157 2.42 0 1.333-.946 2.418-2.157 2.418"
          />
        </svg>
      );
    default:
      return (
        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" className="size-4" aria-hidden>
          <circle cx="12" cy="12" r="10" fill="none" stroke="currentColor" strokeWidth="1.5" />
        </svg>
      );
  }
}
