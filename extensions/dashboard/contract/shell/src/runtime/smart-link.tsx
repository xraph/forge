import * as React from "react";
import { Link } from "react-router-dom";

// SmartLink is a presentational anchor that routes internal paths through
// react-router (so navigation stays inside the SPA and honors the
// BrowserRouter basename) while sending absolute http(s) URLs through a
// plain target=_blank anchor. Same internal/external branch as atom.link,
// extracted so the auth forms (login ↔ signup cross-links) can navigate
// client-side instead of full-reloading out of the shell.

interface SmartLinkProps {
  href: string;
  children: React.ReactNode;
  className?: string;
}

export function SmartLink({ href, children, className }: SmartLinkProps) {
  if (/^https?:\/\//i.test(href)) {
    return (
      <a href={href} target="_blank" rel="noreferrer" className={className}>
        {children}
      </a>
    );
  }
  return (
    <Link to={href} className={className}>
      {children}
    </Link>
  );
}
