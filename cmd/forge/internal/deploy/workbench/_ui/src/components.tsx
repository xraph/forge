import { useEffect, useId, useState, type ReactNode } from "react";
import { LoaderCircle, Moon, Sun } from "lucide-react";
import { Button } from "@forge-go/dashboard-kit/components/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@forge-go/dashboard-kit/components/card";
import { Input } from "@forge-go/dashboard-kit/components/input";
import { Label } from "@forge-go/dashboard-kit/components/label";
import {
  NativeSelect,
  NativeSelectOption,
} from "@forge-go/dashboard-kit/components/native-select";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@forge-go/dashboard-kit/components/tooltip";
export {
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  Input,
  Label,
  NativeSelect,
  NativeSelectOption,
};
export {
  Collapsible,
  CollapsibleTrigger,
  CollapsibleContent,
} from "@forge-go/dashboard-kit/components/collapsible";
export { Badge } from "@forge-go/dashboard-kit/components/badge";
export { Checkbox } from "@forge-go/dashboard-kit/components/checkbox";
export {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@forge-go/dashboard-kit/components/tabs";
export {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@forge-go/dashboard-kit/components/table";
export {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@forge-go/dashboard-kit/components/dialog";
export {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@forge-go/dashboard-kit/components/sheet";
export { ZeroState } from "@forge-go/dashboard-kit/components/zero-state";
export { Textarea } from "@forge-go/dashboard-kit/components/textarea";
export { Separator } from "@forge-go/dashboard-kit/components/separator";
export function IconButton({
  label,
  icon,
  onClick,
  disabled,
  variant = "outline",
}: {
  label: string;
  icon: ReactNode;
  onClick: () => void;
  disabled?: boolean;
  variant?: "outline" | "ghost" | "default" | "destructive";
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={<Button size="icon" variant={variant} disabled={disabled} />}
        aria-label={label}
        onClick={onClick}
      >
        {icon}
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}
export function Panel({
  title,
  description,
  action,
  children,
  className = "",
}: {
  title: string;
  description?: string;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <Card size="sm" className={className}>
      <CardHeader>
        <CardTitle className="text-sm">{title}</CardTitle>
        {description && (
          <CardDescription className="text-xs">{description}</CardDescription>
        )}
        {action && (
          <div className="col-start-2 row-start-1 row-span-2 flex items-center gap-1">
            {action}
          </div>
        )}
      </CardHeader>
      <CardContent>{children}</CardContent>
    </Card>
  );
}
export function Field({
  label,
  help,
  children,
}: {
  label: string;
  help?: string;
  children: ReactNode;
}) {
  const id = useId();
  return (
    <div className="grid min-w-0 gap-1.5">
      <Label htmlFor={id} className="text-xs">
        {label}
      </Label>
      <div id={id}>{children}</div>
      {help && (
        <p className="text-xs leading-relaxed text-muted-foreground">{help}</p>
      )}
    </div>
  );
}
export function TextField({
  label,
  value,
  onChange,
  placeholder,
  type = "text",
  help,
  disabled,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  type?: string;
  help?: string;
  disabled?: boolean;
}) {
  const id = useId();
  return (
    <div className="grid min-w-0 gap-1.5">
      <Label htmlFor={id} className="text-xs">
        {label}
      </Label>
      <Input
        id={id}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        type={type}
        disabled={disabled}
        autoComplete={type === "password" ? "new-password" : "off"}
      />
      {help && (
        <p className="text-xs leading-relaxed text-muted-foreground">{help}</p>
      )}
    </div>
  );
}
export function SelectField({
  label,
  value,
  onChange,
  options,
  help,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  options: { value: string; label: string }[];
  help?: string;
}) {
  const id = useId();
  return (
    <div className="grid min-w-0 gap-1.5">
      <Label htmlFor={id} className="text-xs">
        {label}
      </Label>
      <NativeSelect
        id={id}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="w-full"
      >
        {options.map((o) => (
          <NativeSelectOption key={o.value} value={o.value}>
            {o.label}
          </NativeSelectOption>
        ))}
      </NativeSelect>
      {help && (
        <p className="text-xs leading-relaxed text-muted-foreground">{help}</p>
      )}
    </div>
  );
}
export function Spinner() {
  return <LoaderCircle className="size-4 animate-spin" aria-label="Working" />;
}
export function preference(key: string) {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}
export function persistPreference(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    /* Configuration remains authoritative on the server. */
  }
}
export function ThemePicker() {
  const [theme, setTheme] = useState(
    () => preference("forge-deploy:theme") ?? "system",
  );
  const [systemDark, setSystemDark] = useState(
    () => window.matchMedia("(prefers-color-scheme: dark)").matches,
  );
  useEffect(() => {
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const update = () => setSystemDark(media.matches);
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);
  const dark = theme === "dark" || (theme === "system" && systemDark);
  useEffect(() => {
    document.documentElement.classList.toggle("dark", dark);
  }, [dark]);
  return (
    <IconButton
      label={dark ? "Switch to light theme" : "Switch to dark theme"}
      icon={dark ? <Sun className="size-4" /> : <Moon className="size-4" />}
      variant="ghost"
      onClick={() => {
        const nextTheme = dark ? "light" : "dark";
        setTheme(nextTheme);
        persistPreference("forge-deploy:theme", nextTheme);
      }}
    />
  );
}
