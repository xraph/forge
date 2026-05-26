import { Link } from "react-router-dom";
import { Icon } from "./atom.icon";
import { cn } from "@/lib/utils";
import { textVariantClass, type TextVariant } from "@/lib/tokens";
import type { IntentComponentProps } from "../runtime/registry";

interface LinkProps {
  text: string;
  href: string;
  external?: boolean;
  variant?: TextVariant;
  icon?: string;
  className?: string;
}

export function AtomLink({ props }: IntentComponentProps<unknown, LinkProps>) {
  const variantCls = textVariantClass(props.variant ?? "primary");
  const cls = cn(
    "inline-flex items-center gap-1 underline-offset-4 hover:underline",
    variantCls,
    props.className,
  );

  if (props.external || /^https?:\/\//i.test(props.href)) {
    return (
      <a href={props.href} target="_blank" rel="noreferrer" className={cls}>
        {props.icon ? (
          <Icon node={{} as never} slots={{}} props={{ name: props.icon, size: 14 }} />
        ) : null}
        {props.text}
      </a>
    );
  }
  return (
    <Link to={props.href} className={cls}>
      {props.icon ? (
        <Icon node={{} as never} slots={{}} props={{ name: props.icon, size: 14 }} />
      ) : null}
      {props.text}
    </Link>
  );
}
