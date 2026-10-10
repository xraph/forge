import { useEffect, useState } from "react";
import {
  Activity,
  ArrowRight,
  Check,
  CheckCheck,
  Clipboard,
  ChevronDown,
  CircleAlert,
  Database,
  Download,
  FileCode2,
  GitBranch,
  HardDrive,
  KeyRound,
  LockKeyhole,
  Network,
  Package,
  Plus,
  RefreshCw,
  Rocket,
  Server,
  Settings2,
  ShieldCheck,
  Trash2,
  Undo2,
  Upload,
  X,
} from "lucide-react";
import type { Connection, Diagnostic, Lifecycle, Service } from "./api";
import type { Workspace } from "./controller";
import {
  Badge,
  Button,
  Checkbox,
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  IconButton,
  Input,
  NativeSelect,
  NativeSelectOption,
  Panel,
  SelectField,
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  Spinner,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  TextField,
  Textarea,
  ZeroState,
} from "./components";
function jsonError(error: unknown) {
  return error instanceof Error ? error.message : "Invalid configuration";
}
function CopyButton({
  text,
  label = "Copy",
}: {
  text: string;
  label?: string;
}) {
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState("");
  return (
    <div className="flex items-center gap-1">
      <IconButton
        label={copied ? "Copied" : label}
        icon={copied ? <Check /> : <Clipboard />}
        onClick={() => {
          navigator.clipboard
            .writeText(text)
            .then(() => {
              setCopied(true);
              setError("");
            })
            .catch(() => setError("Select and copy the text below."));
        }}
        variant="ghost"
      />
      {error && (
        <span role="status" className="text-xs text-muted-foreground">
          {error}
        </span>
      )}
    </div>
  );
}
function Summary({ w }: { w: Workspace }) {
  const resourceCount = Object.keys(w.draft?.resources ?? {}).length;
  return (
    <div className="grid grid-cols-2 gap-3 xl:grid-cols-4">
      {[
        {
          title: "Services in scope",
          value: `${w.selected.length} / ${Object.keys(w.draft?.services ?? {}).length}`,
          icon: Package,
        },
        {
          title: "Data resources",
          value: String(resourceCount),
          icon: Database,
        },
        {
          title: "Delivery",
          value:
            w.target?.build?.delivery ??
            (w.target?.provider === "compose" ? "local" : "registry"),
          icon: GitBranch,
        },
        {
          title: "Settings",
          value: w.project?.settings.store.backend ?? "files",
          icon: HardDrive,
        },
      ].map((item) => (
        <Panel
          key={item.title}
          title={item.title}
          action={<item.icon className="size-4 text-muted-foreground" />}
        >
          <p className="truncate text-lg font-medium tracking-tight">
            {item.value}
          </p>
        </Panel>
      ))}
    </div>
  );
}
export function OverviewView({ w }: { w: Workspace }) {
  return (
    <>
      <Summary w={w} />
      {Boolean(w.project?.diagnostics?.length) && (
        <Collapsible className="rounded-lg border bg-card">
          <CollapsibleTrigger className="group flex min-h-9 w-full items-center gap-2 px-3 py-2 text-left text-xs focus-visible:outline-2 focus-visible:outline-ring">
            <CircleAlert className="size-4 text-amber-600 dark:text-amber-400" />
            <span className="flex-1">
              {w.project?.diagnostics.length} discovery notices. Review before
              planning.
            </span>
            <ChevronDown className="size-4 transition-transform group-aria-expanded:rotate-180" />
          </CollapsibleTrigger>
          <CollapsibleContent className="px-3 pb-3">
            <Diagnostics diagnostics={w.project?.diagnostics ?? []} />
          </CollapsibleContent>
        </Collapsible>
      )}
      <ServicesView w={w} />
      <ResourceView w={w} compact />
      <ConnectionsSummary w={w} />
    </>
  );
}
export function ServicesView({ w }: { w: Workspace }) {
  const [selectedService, setSelectedService] = useState("");
  const services = Object.entries(w.draft?.services ?? {});
  const service = w.draft?.services[selectedService];
  return (
    <>
      <Panel
        title="Application services"
        description={
          w.target?.resource_only
            ? "This companion target deploys the environment's resources. Application selection is disabled."
            : "Select the services to roll out. Other recorded workloads and shared data stay in place."
        }
        action={<Badge variant="outline">{w.selected.length} selected</Badge>}
      >
        {services.length ? (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Service</TableHead>
                <TableHead>Exposure</TableHead>
                <TableHead>Replicas</TableHead>
                <TableHead>Bindings</TableHead>
                <TableHead className="w-9" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {services.map(([name, s]) => {
                const ports = Object.values(s.ports ?? {});
                const exposure = ports.some((p) => p.exposure === "public")
                  ? "Public"
                  : s.kind === "worker"
                    ? "Outbound"
                    : "Private";
                return (
                  <TableRow key={name}>
                    <TableCell>
                      <div className="flex items-center gap-2.5">
                        <Checkbox
                          aria-label={`Deploy ${name}`}
                          checked={w.selected.includes(name)}
                          disabled={w.busy || Boolean(w.target?.resource_only)}
                          onCheckedChange={(checked) => {
                            const next = checked
                              ? [...w.selected, name]
                              : w.selected.filter((item) => item !== name);
                            if (next.length === 0 && !w.target?.resource_only) {
                              w.report(
                                new Error(
                                  "Select at least one service, or configure a resource-only target.",
                                ),
                              );
                              return;
                            }
                            w.edit(
                              `deploy.environments.${w.environment}.services`,
                              next,
                            );
                          }}
                        />
                        <div className="min-w-0">
                          <Button
                            variant="link"
                            size="sm"
                            className="h-auto p-0 font-medium"
                            onClick={() => setSelectedService(name)}
                          >
                            {name}
                          </Button>
                          <p className="text-[11px] text-muted-foreground">
                            {s.kind} · {s.app}
                          </p>
                        </div>
                      </div>
                    </TableCell>
                    <TableCell>
                      <Badge variant="outline">{exposure}</Badge>
                      <p className="mt-1 text-[11px] text-muted-foreground">
                        {ports.map((p) => p.port).join(", ") || "No listener"}
                      </p>
                    </TableCell>
                    <TableCell>
                      <Input
                        aria-label={`${name} replicas`}
                        type="number"
                        min={1}
                        max={1000}
                        className="h-7 w-16"
                        value={w.env?.replicas?.[name] ?? s.replicas ?? 1}
                        onChange={(e) =>
                          w.edit(
                            `deploy.environments.${w.environment}.replicas.${name}`,
                            Number(e.target.value),
                          )
                        }
                      />
                    </TableCell>
                    <TableCell>
                      <div className="flex max-w-48 flex-wrap gap-1">
                        {s.bindings?.map((b, i) => (
                          <Badge
                            variant="secondary"
                            key={i}
                            className="text-[10px]"
                          >
                            {b.resource}
                          </Badge>
                        ))}
                        {!s.bindings?.length && (
                          <span className="text-xs text-muted-foreground">
                            None
                          </span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      <IconButton
                        label={`Configure ${name}`}
                        icon={<Settings2 />}
                        onClick={() => setSelectedService(name)}
                        variant="ghost"
                      />
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        ) : (
          <ZeroState
            title="No application services"
            body="Discover services from your Forge project, then review their suggested configuration."
            illustration={<Package className="size-6" />}
            action={
              <Button
                size="sm"
                onClick={() =>
                  void w.act(async () => {
                    await w.api.request("init", { answers: {}, force: false });
                    await w.load();
                  })
                }
              >
                Discover services
              </Button>
            }
          />
        )}
      </Panel>
      {service && (
        <ServiceSheet
          key={selectedService}
          w={w}
          name={selectedService}
          service={service}
          onClose={() => setSelectedService("")}
        />
      )}
    </>
  );
}
function ServiceSheet({
  w,
  name,
  service,
  onClose,
}: {
  w: Workspace;
  name: string;
  service: Service;
  onClose: () => void;
}) {
  const [kind, setKind] = useState(service.kind);
  const [health, setHealth] = useState(
    JSON.stringify(service.health ?? {}, null, 2),
  );
  const [bindings, setBindings] = useState(
    JSON.stringify(service.bindings ?? [], null, 2),
  );
  const [calls, setCalls] = useState((service.calls ?? []).join(", "));
  const [migrate, setMigrate] = useState(service.migrate ?? "");
  const [error, setError] = useState("");
  return (
    <Sheet
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <SheetContent className="overflow-y-auto">
        <SheetHeader>
          <SheetTitle>{name}</SheetTitle>
          <SheetDescription>
            Application settings from your deployment specification.
          </SheetDescription>
        </SheetHeader>
        <div className="space-y-4 px-4 pb-4">
          <SelectField
            label="Workload kind"
            value={kind}
            onChange={setKind}
            options={["web", "worker", "cron"].map((value) => ({
              value,
              label: value,
            }))}
          />
          <TextField
            label="Calls these services"
            value={calls}
            onChange={setCalls}
            help="Comma-separated logical service names. Replicas keep their own instance identity."
          />
          <TextField
            label="Migration entry point"
            value={migrate}
            onChange={setMigrate}
            placeholder="auto"
          />
          <label className="grid gap-2 text-xs">
            Health configuration (JSON)
            <Textarea
              aria-label="Health configuration"
              rows={5}
              className="font-mono text-xs"
              value={health}
              onChange={(e) => setHealth(e.target.value)}
            />
          </label>
          <label className="grid gap-2 text-xs">
            Resource bindings (JSON)
            <Textarea
              aria-label="Resource bindings"
              rows={8}
              className="font-mono text-xs"
              value={bindings}
              onChange={(e) => setBindings(e.target.value)}
            />
          </label>
          {error && (
            <p role="alert" className="text-xs text-destructive">
              {error}
            </p>
          )}
          <Button
            size="sm"
            onClick={() => {
              try {
                const healthValue = JSON.parse(health);
                const bindingValue = JSON.parse(bindings);
                if (
                  !Array.isArray(bindingValue) ||
                  !healthValue ||
                  Array.isArray(healthValue) ||
                  typeof healthValue !== "object"
                )
                  throw new Error("Use a health object and a bindings array");
                w.edit(`deploy.services.${name}`, {
                  ...service,
                  kind,
                  health: healthValue,
                  bindings: bindingValue,
                  calls: calls
                    .split(",")
                    .map((v) => v.trim())
                    .filter(Boolean),
                  migrate,
                });
                onClose();
              } catch (e) {
                setError(jsonError(e));
              }
            }}
          >
            <Check />
            Update draft
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}
export function ResourceView({
  w,
  compact = false,
}: {
  w: Workspace;
  compact?: boolean;
}) {
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState("");
  const resources = Object.entries(w.draft?.resources ?? {});
  return (
    <>
      <Panel
        title="Data & messaging"
        description={
          compact
            ? "Grove databases, Trove storage, cache and service brokers."
            : "Choose a backend and place each dependency on this target, an existing service or a companion target."
        }
        action={
          <IconButton
            label="Add resource"
            icon={<Plus />}
            onClick={() => setAdding(true)}
          />
        }
      >
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Resource</TableHead>
              <TableHead>Backend</TableHead>
              <TableHead>Placement</TableHead>
              <TableHead className="w-9" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {resources.map(([name, r]) => {
              const placement = w.env?.resources?.[name] ?? {};
              const consumers = Object.entries(w.draft?.services ?? {})
                .filter(([, s]) => s.bindings?.some((b) => b.resource === name))
                .map(([service]) => service);
              return (
                <TableRow key={name}>
                  <TableCell>
                    <div className="font-medium">{name}</div>
                    <p className="mt-0.5 text-[11px] text-muted-foreground">
                      {consumers.join(", ") || "No application binding"}
                    </p>
                  </TableCell>
                  <TableCell>
                    <span className="text-xs">
                      {r.type}
                      {r.version ? ` ${r.version}` : ""}
                    </span>
                    {r.features?.length && (
                      <p className="mt-1 text-[11px] text-muted-foreground">
                        {r.features.join(" + ")}
                      </p>
                    )}
                  </TableCell>
                  <TableCell>
                    <NativeSelect
                      aria-label={`${name} placement`}
                      value={placement.lifecycle ?? ""}
                      onChange={(e) =>
                        w.edit(
                          `deploy.environments.${w.environment}.resources.${name}`,
                          { ...placement, lifecycle: e.target.value },
                        )
                      }
                      size="sm"
                    >
                      <NativeSelectOption value="" disabled>
                        Choose placement
                      </NativeSelectOption>
                      <NativeSelectOption value="container">
                        Self-hosted
                      </NativeSelectOption>
                      <NativeSelectOption value="managed">
                        Managed service
                      </NativeSelectOption>
                      <NativeSelectOption value="external">
                        Existing service
                      </NativeSelectOption>
                    </NativeSelect>
                    {placement.target && (
                      <p className="mt-1 text-[11px] text-muted-foreground">
                        Companion: {placement.target}
                      </p>
                    )}
                  </TableCell>
                  <TableCell>
                    <IconButton
                      label={`Configure ${name}`}
                      icon={<Settings2 />}
                      onClick={() => setEditing(name)}
                      variant="ghost"
                    />
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
        {!resources.length && (
          <ZeroState
            title="No data resources"
            body="Add the database, storage or messaging dependencies your services need."
            illustration={<Database className="size-6" />}
            action={
              <Button size="sm" onClick={() => setAdding(true)}>
                Add resource
              </Button>
            }
          />
        )}
      </Panel>
      {!compact && (
        <Panel
          title="Resource placement"
          description="Use a companion Compose or Kubernetes target when your application platform cannot host a broker or storage backend."
        >
          <p className="text-xs leading-relaxed text-muted-foreground">
            The compiler validates the backend, required Redis features, Trove
            metadata database and service bindings before generating artifacts.
            Managed placement must be supported by the selected provider.
          </p>
        </Panel>
      )}
      <ResourceDialog
        w={w}
        name={editing}
        adding={adding}
        open={adding || !!editing}
        onClose={() => {
          setAdding(false);
          setEditing("");
        }}
      />
    </>
  );
}
function ResourceDialog({
  w,
  name,
  adding,
  open,
  onClose,
}: {
  w: Workspace;
  name: string;
  adding: boolean;
  open: boolean;
  onClose: () => void;
}) {
  const [resourceName, setResourceName] = useState("");
  const current = w.draft?.resources?.[name];
  const [type, setType] = useState("postgres");
  const [version, setVersion] = useState("16");
  const [features, setFeatures] = useState("");
  const placement = w.env?.resources?.[name] ?? {};
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        if (!value) onClose();
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {adding ? "Add a dependency" : `Configure ${name}`}
          </DialogTitle>
          <DialogDescription>
            {adding
              ? "Use explicit versions and bindings so the same dependency can move between platforms."
              : "Placement changes affect this environment. Backend changes belong to your resource specification."}
          </DialogDescription>
        </DialogHeader>
        {adding ? (
          <>
            <TextField
              label="Resource name"
              value={resourceName}
              onChange={setResourceName}
              placeholder="jobs"
            />
            <SelectField
              label="Backend"
              value={type}
              onChange={(value) => {
                setType(value);
                setVersion(
                  value === "postgres"
                    ? "16"
                    : value === "nats"
                      ? "2.11"
                      : value === "rabbitmq"
                        ? "4.1"
                        : "",
                );
              }}
              options={[
                "postgres",
                "mysql",
                "sqlite",
                "redis",
                "object-storage",
                "nats",
                "rabbitmq",
                "kafka",
              ].map((value) => ({ value, label: value }))}
            />
            <TextField
              label="Backend version"
              value={version}
              onChange={setVersion}
            />
            <TextField
              label="Required features"
              value={features}
              onChange={setFeatures}
              placeholder="json, search"
            />
            <p className="text-xs text-muted-foreground">
              After adding a broker, bind the participating services and choose
              its environment placement.
            </p>
          </>
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant="outline">{current?.type}</Badge>
              <span className="text-xs text-muted-foreground">
                {current?.version ?? "Version not set"}
              </span>
            </div>
            <SelectField
              label="Companion target"
              value={placement.target ?? ""}
              onChange={(value) =>
                w.edit(
                  `deploy.environments.${w.environment}.resources.${name}`,
                  {
                    ...placement,
                    ...(value ? { target: value } : { target: "" }),
                  },
                )
              }
              options={[
                { value: "", label: "This deployment target" },
                ...Object.entries(w.draft?.targets ?? {})
                  .filter(([key]) => key !== w.profile)
                  .map(([value, t]) => ({
                    value,
                    label: `${value} (${t.provider})`,
                  })),
              ]}
            />
            <TextField
              label="Credential reference"
              value={placement.secret ?? ""}
              onChange={(value) =>
                w.edit(
                  `deploy.environments.${w.environment}.resources.${name}`,
                  { ...placement, secret: value },
                )
              }
              placeholder="primary-dsn"
              help="Reference a secret in your configured resolver. Credential values stay private."
            />
            {w.target?.provider === "digitalocean" &&
              placement.lifecycle === "managed" && (
                <TextField
                  label="Existing database cluster"
                  value={w.target.managed_databases?.[name] ?? ""}
                  onChange={(cluster) =>
                    w.edit(`deploy.targets.${w.profile}.managed_databases`, {
                      ...w.target?.managed_databases,
                      [name]: cluster,
                    })
                  }
                  placeholder="atlas-pg"
                  help="App Platform attaches a provisioned production database. Forge does not create this cluster."
                />
              )}
            <TextField
              label="Container recipe"
              value={placement.recipe ?? ""}
              onChange={(value) =>
                w.edit(
                  `deploy.environments.${w.environment}.resources.${name}`,
                  { ...placement, recipe: value },
                )
              }
              placeholder="redis-stack"
            />
            {current?.type === "redis" && (
              <Button
                size="sm"
                variant="outline"
                onClick={() =>
                  w.edit(`deploy.resources.${name}.features`, [
                    "json",
                    "search",
                  ])
                }
              >
                Require Redis JSON + search
              </Button>
            )}
          </>
        )}
        <DialogFooter>
          <Button size="sm" variant="outline" onClick={onClose}>
            Close
          </Button>
          {adding && (
            <Button
              size="sm"
              disabled={!resourceName}
              onClick={() => {
                if (
                  !/^[a-z][a-z0-9-]{0,62}$/.test(resourceName) ||
                  w.draft?.resources?.[resourceName]
                ) {
                  w.report(
                    new Error("Choose a unique lowercase resource name"),
                  );
                  return;
                }
                w.edit(`deploy.resources.${resourceName}`, {
                  type,
                  ...(version ? { version } : {}),
                  ...(features
                    ? {
                        features: features
                          .split(",")
                          .map((v) => v.trim())
                          .filter(Boolean),
                      }
                    : {}),
                });
                onClose();
              }}
            >
              <Plus />
              Add to draft
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
function ConnectionsSummary({ w }: { w: Workspace }) {
  const connections = w.draft?.connections ?? [];
  const calls = Object.entries(w.draft?.services ?? {}).flatMap(([from, s]) =>
    (s.calls ?? []).map((to) => ({ from, to })),
  );
  return (
    <Panel
      title="Service communication"
      action={
        <IconButton
          label="Configure connections"
          icon={<Network />}
          onClick={() => w.setSection("connections")}
        />
      }
    >
      <div className="flex flex-wrap gap-2">
        {[...calls, ...connections].map((c, i) => (
          <Badge variant="outline" key={i}>
            {c.from}
            <ArrowRight className="mx-1 size-3" />
            {c.to}
          </Badge>
        ))}
        {!calls.length && !connections.length && (
          <p className="text-xs text-muted-foreground">
            No declared service calls.
          </p>
        )}
      </div>
    </Panel>
  );
}
export function ConnectionsView({ w }: { w: Workspace }) {
  const [json, setJSON] = useState(
    JSON.stringify(w.draft?.connections ?? [], null, 2),
  );
  return (
    <>
      <ConnectionsSummary w={w} />
      <Panel
        title="Connection overrides"
        description="Configure ports, runtime URL keys, timeouts and retry limits between logical services."
      >
        <Textarea
          aria-label="Connection overrides"
          className="min-h-44 font-mono text-xs"
          value={json}
          onChange={(e) => setJSON(e.target.value)}
        />
        <div className="mt-3 flex items-center justify-between gap-2">
          <p className="text-xs text-muted-foreground">
            The compiler generates platform-specific service addresses.
          </p>
          <IconButton
            label="Update connections draft"
            icon={<Check />}
            onClick={() => {
              try {
                const value = JSON.parse(json);
                if (!Array.isArray(value))
                  throw new Error("Use an array of connection objects");
                w.edit("deploy.connections", value);
              } catch (error) {
                w.report(error);
              }
            }}
          />
        </div>
      </Panel>
      <Panel
        title="External service endpoints"
        description="Use explicit endpoints when a dependency runs outside this environment."
      >
        <div className="space-y-3">
          {Object.entries(w.env?.external_services ?? {}).map(
            ([name, value]) => (
              <TextField
                key={name}
                label={`${name} endpoint`}
                value={value.url}
                onChange={(url) =>
                  w.edit(
                    `deploy.environments.${w.environment}.external_services.${name}`,
                    { ...value, url },
                  )
                }
              />
            ),
          )}
          {!Object.keys(w.env?.external_services ?? {}).length && (
            <ZeroState
              title="No external service endpoints"
              body="Your declared calls will use the selected platform's service network. Add explicit external endpoints in the environment configuration when needed."
              illustration={<Network className="size-6" />}
            />
          )}
        </div>
      </Panel>
    </>
  );
}
export function ImagesView({ w }: { w: Workspace }) {
  const [registryOpen, setRegistryOpen] = useState(false);
  const build = w.target?.build ?? {};
  const delivery = ["git", "existing", "ci"].includes(build.source ?? "")
    ? build.source === "git"
      ? "git"
      : "existing"
    : (build.delivery ??
      (w.target?.provider === "compose" ? "local" : "registry"));
  const update = (value: typeof build) =>
    w.edit(`deploy.targets.${w.profile}.build`, value);
  const registry = build.registry ?? {};
  return (
    <>
      <Panel
        title="Image delivery"
        description="Choose where images are built and how your platform receives them."
        action={
          <div className="flex items-center gap-1">
            {w.publication && (
              <IconButton
                label="Use published images"
                icon={<CheckCheck />}
                onClick={w.usePublishedImages}
                disabled={w.busy || w.dirty}
              />
            )}
            <IconButton
              label="Publish reviewed images"
              icon={<Upload />}
              onClick={() => void w.publishImages()}
              disabled={!w.canPublish || !w.approved || w.busy || w.dirty}
            />
          </div>
        }
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <SelectField
            label="Delivery method"
            value={delivery}
            onChange={(value) =>
              update({
                ...build,
                source:
                  value === "git" || value === "existing"
                    ? value
                    : ["local", "remote"].includes(build.source ?? "")
                      ? build.source
                      : "local",
                delivery: value === "existing" ? "registry" : value,
                ...(value === "git" ? { trigger: build.trigger ?? "off" } : {}),
              })
            }
            options={[
              { value: "local", label: "Build on this machine" },
              { value: "registry", label: "Build and publish to a registry" },
              { value: "existing", label: "Use existing images" },
              { value: "git", label: "Platform builds from Git" },
            ]}
          />
          {!["git", "existing"].includes(delivery) && (
            <SelectField
              label="Build source"
              value={build.source ?? "local"}
              onChange={(source) => update({ ...build, source })}
              options={[
                { value: "local", label: "Local Docker" },
                { value: "remote", label: "Named Buildx builder" },
              ]}
            />
          )}
          {build.source === "remote" &&
            !["git", "existing"].includes(delivery) && (
              <TextField
                label="Buildx builder"
                value={build.builder ?? ""}
                onChange={(builder) => update({ ...build, builder })}
                placeholder="your-configured-builder"
                help="Configure this builder in Docker before deploying."
              />
            )}
          {delivery === "registry" && (
            <>
              <TextField
                label="Registry host"
                value={registry.host ?? ""}
                onChange={(host) =>
                  update({ ...build, registry: { ...registry, host } })
                }
                placeholder="ghcr.io"
              />
              <TextField
                label="Registry namespace"
                value={registry.namespace ?? ""}
                onChange={(namespace) =>
                  update({ ...build, registry: { ...registry, namespace } })
                }
                placeholder="your-organization"
              />
              <SelectField
                label="Registry visibility"
                value={registry.visibility ?? "private"}
                onChange={(visibility) =>
                  update({ ...build, registry: { ...registry, visibility } })
                }
                options={[
                  { value: "private", label: "Private" },
                  { value: "public", label: "Public" },
                ]}
              />
              <TextField
                label="Push authentication reference"
                value={registry.auth ?? ""}
                onChange={(auth) =>
                  update({ ...build, registry: { ...registry, auth } })
                }
                placeholder="ghcr"
              />
            </>
          )}
          {delivery === "git" && (
            <>
              <TextField
                label="Source repository"
                value={build.repo ?? ""}
                onChange={(repo) => update({ ...build, repo })}
                placeholder="https://github.com/your-org/your-project"
              />
              <TextField
                label="Source branch"
                value={build.branch ?? "main"}
                onChange={(branch) => update({ ...build, branch })}
              />
              <TextField
                label="Source commit"
                value={build.commit ?? ""}
                onChange={(commit) => update({ ...build, commit })}
                help="Pin the reviewed commit before plan approval."
              />
              <SelectField
                label="Deploy trigger"
                value={build.trigger ?? "off"}
                onChange={(trigger) => update({ ...build, trigger })}
                options={[
                  { value: "off", label: "Manual" },
                  { value: "commit", label: "On commit" },
                  { value: "checksPass", label: "After checks pass" },
                ]}
              />
            </>
          )}
          {delivery === "existing" && (
            <div className="sm:col-span-2 grid gap-3">
              {Object.keys(w.draft?.services ?? {}).map((service) => (
                <TextField
                  key={service}
                  label={`${service} image`}
                  value={build.images?.[service] ?? ""}
                  onChange={(image) =>
                    update({
                      ...build,
                      images: { ...build.images, [service]: image },
                    })
                  }
                  placeholder="registry.example.com/api@sha256:…"
                />
              ))}
            </div>
          )}
          {delivery !== "git" && (
            <TextField
              label="Image platforms"
              value={(build.platforms ?? []).join(", ")}
              onChange={(platforms) =>
                update({
                  ...build,
                  platforms: platforms
                    .split(",")
                    .map((v) => v.trim())
                    .filter(Boolean),
                })
              }
              placeholder="linux/amd64, linux/arm64"
            />
          )}
        </div>
        {delivery === "git" &&
          !["render", "digitalocean"].includes(w.target?.provider ?? "") && (
            <p className="mt-3 text-xs text-warning">
              Provider Git builds require a compatible platform adapter.
              Preflight rejects unsupported delivery.
            </p>
          )}
      </Panel>
      {delivery === "registry" && (
        <Panel
          title="Registry credentials"
          description="Authenticate image publishing on this machine. Cluster image pulls use their own secret reference."
          action={
            <IconButton
              label="Authenticate registry"
              icon={<KeyRound />}
              onClick={() => setRegistryOpen(true)}
            />
          }
        >
          <div className="grid gap-4 sm:grid-cols-2">
            <TextField
              label="Kubernetes pull secret"
              value={registry.pull_secret ?? ""}
              onChange={(pull_secret) =>
                update({ ...build, registry: { ...registry, pull_secret } })
              }
              placeholder="forge-registry"
            />
            <TextField
              label="Provider image credential reference"
              value={registry.secret_ref ?? ""}
              onChange={(secret_ref) =>
                update({ ...build, registry: { ...registry, secret_ref } })
              }
              placeholder="image-registry"
            />
          </div>
        </Panel>
      )}
      {delivery === "git" && (
        <Panel
          title="Service build settings"
          description="Keep per-service roots, Dockerfiles and start commands explicit for monorepo builds."
        >
          <div className="space-y-4">
            {Object.keys(w.draft?.services ?? {}).map((service) => {
              const config = build.services?.[service] ?? {};
              return (
                <div
                  key={service}
                  className="grid gap-3 border-b pb-4 last:border-0 last:pb-0 sm:grid-cols-2"
                >
                  <h3 className="text-sm font-medium sm:col-span-2">
                    {service}
                  </h3>
                  {(
                    [
                      "root_dir",
                      "dockerfile",
                      "build_command",
                      "start_command",
                    ] as const
                  ).map((field) => (
                    <TextField
                      key={field}
                      label={`${service} ${field.replaceAll("_", " ")}`}
                      value={config[field] ?? ""}
                      onChange={(value) =>
                        update({
                          ...build,
                          services: {
                            ...build.services,
                            [service]: { ...config, [field]: value },
                          },
                        })
                      }
                    />
                  ))}
                </div>
              );
            })}
          </div>
        </Panel>
      )}
      {w.target?.provider === "kubernetes" ? (
        <Panel
          title="GitOps release"
          description="Export deployment artifacts for a repository and controller workflow."
        >
          <div className="grid gap-4 sm:grid-cols-2">
            <SelectField
              label="Release mode"
              value={w.target?.release?.mode ?? "direct"}
              onChange={(mode) =>
                w.edit(`deploy.targets.${w.profile}.release`, {
                  ...w.target?.release,
                  mode,
                })
              }
              options={[
                { value: "direct", label: "Apply directly" },
                { value: "gitops", label: "GitOps export / handoff" },
              ]}
            />
            {w.target?.release?.mode === "gitops" && (
              <>
                <SelectField
                  label="GitOps controller"
                  value={w.target.release.controller ?? "generic"}
                  onChange={(controller) =>
                    w.edit(`deploy.targets.${w.profile}.release`, {
                      ...w.target?.release,
                      controller,
                      approval: "manual",
                    })
                  }
                  options={[
                    { value: "generic", label: "Existing controller" },
                    { value: "argo-cd", label: "Argo CD" },
                    { value: "flux", label: "Flux" },
                  ]}
                />
                <TextField
                  label="Release repository"
                  value={w.target.release.repo ?? ""}
                  onChange={(repo) =>
                    w.edit(`deploy.targets.${w.profile}.release`, {
                      ...w.target?.release,
                      repo,
                    })
                  }
                />
                <TextField
                  label="Release branch"
                  value={w.target.release.branch ?? "main"}
                  onChange={(branch) =>
                    w.edit(`deploy.targets.${w.profile}.release`, {
                      ...w.target?.release,
                      branch,
                    })
                  }
                />
                <TextField
                  label="Artifact path"
                  value={w.target.release.path ?? ""}
                  onChange={(path) =>
                    w.edit(`deploy.targets.${w.profile}.release`, {
                      ...w.target?.release,
                      path,
                    })
                  }
                />
              </>
            )}
          </div>
          {w.target?.release?.mode === "gitops" && (
            <p className="mt-3 text-xs text-muted-foreground">
              Use immutable registry images and existing data services. Export
              includes required Secret names and keys. Review a manual sync in
              your controller with pruning disabled. Migrations and one-off jobs
              require direct deployment.
            </p>
          )}
        </Panel>
      ) : (
        <Panel
          title="Release handoff"
          description="Provider Git builds use the source repository and deploy trigger above."
        >
          <p className="text-xs text-muted-foreground">
            Export the reviewed files, then deploy through your provider.
            Platform build triggers are separate from a Kubernetes controller
            watching manifests.
          </p>
        </Panel>
      )}
      <RegistryDialog
        w={w}
        open={registryOpen}
        onClose={() => setRegistryOpen(false)}
      />
    </>
  );
}
function RegistryDialog({
  w,
  open,
  onClose,
}: {
  w: Workspace;
  open: boolean;
  onClose: () => void;
}) {
  const registry = w.target?.build?.registry;
  const [name, setName] = useState(registry?.auth ?? "ghcr");
  const [host, setHost] = useState(registry?.host ?? "ghcr.io");
  const [username, setUsername] = useState("");
  const [token, setToken] = useState("");
  const [connections, setConnections] = useState<Connection[]>([]);
  useEffect(() => {
    if (open)
      w.api
        .request<Connection[]>("connections")
        .then((data) => setConnections(data ?? []))
        .catch(w.report);
  }, [open, w.api, w.report]);
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        if (!value) {
          setToken("");
          onClose();
        }
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Authenticate image publishing</DialogTitle>
          <DialogDescription>
            Forge runs registry login with isolated credentials. The response
            and project settings contain metadata only.
          </DialogDescription>
        </DialogHeader>
        {connections.map((connection) => (
          <Badge variant="outline" key={connection.name}>
            {connection.name}:{" "}
            {connection.connected ? "Connected" : "Not connected"}
          </Badge>
        ))}
        <TextField label="Connection name" value={name} onChange={setName} />
        <TextField
          label="Registry login host"
          value={host}
          onChange={setHost}
        />
        <TextField
          label="Registry username"
          value={username}
          onChange={setUsername}
        />
        <TextField
          label="Registry token"
          value={token}
          onChange={setToken}
          type="password"
          help="Use a token with image publishing permissions. Forge never saves it in browser storage."
        />
        <DialogFooter>
          <Button
            size="sm"
            variant="outline"
            onClick={() => {
              setToken("");
              onClose();
            }}
          >
            Cancel
          </Button>
          <Button
            size="sm"
            disabled={w.busy || !name || !host || !username || !token}
            onClick={async () => {
              const value = token;
              setToken("");
              if (
                await w.act(async () => {
                  await w.api.request("connections/registry", {
                    name,
                    host,
                    username,
                    token: value,
                  });
                  return true;
                })
              )
                onClose();
            }}
          >
            <KeyRound />
            Connect
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
export function EnvironmentView({ w }: { w: Workspace }) {
  const [backend, setBackend] = useState(
    w.project?.settings.store.backend ?? "files",
  );
  const [reference, setReference] = useState(
    w.project?.settings.store.reference ?? "",
  );
  const secrets = w.draft?.secrets ?? {};
  return (
    <Tabs defaultValue="environment">
      <TabsList>
        <TabsTrigger value="environment">Environment</TabsTrigger>
        <TabsTrigger value="storage">Settings storage</TabsTrigger>
        <TabsTrigger value="secrets">Secrets</TabsTrigger>
      </TabsList>
      <TabsContent value="environment">
        <Panel
          title={w.environment}
          description="Environment settings belong to this deployment profile."
        >
          <div className="grid gap-4 sm:grid-cols-2">
            <TextField
              label="Environment purpose"
              value={w.env?.purpose ?? ""}
              onChange={(purpose) =>
                w.edit(`deploy.environments.${w.environment}.purpose`, purpose)
              }
              placeholder="preview, staging, production"
            />
            <SelectField
              label="Environment action"
              value={w.env?.environment_action ?? "reuse"}
              onChange={(value) =>
                w.edit(
                  `deploy.environments.${w.environment}.environment_action`,
                  value,
                )
              }
              options={[
                { value: "reuse", label: "Use existing environment" },
                { value: "create", label: "Create environment if supported" },
              ]}
            />
            {w.target?.provider === "kubernetes" && (
              <>
                <TextField
                  label="Kubernetes context"
                  value={w.target.context ?? ""}
                  onChange={(context) =>
                    w.edit(`deploy.targets.${w.profile}.context`, context)
                  }
                />
                <TextField
                  label="Namespace"
                  value={w.target.namespace ?? ""}
                  onChange={(namespace) =>
                    w.edit(`deploy.targets.${w.profile}.namespace`, namespace)
                  }
                />
              </>
            )}
            {w.target?.provider === "compose" && (
              <TextField
                label="Docker context"
                value={w.target.docker_context ?? ""}
                onChange={(value) =>
                  w.edit(`deploy.targets.${w.profile}.docker_context`, value)
                }
                help="Leave empty to use Docker's current context. Forge does not change it."
              />
            )}
            <TextField
              label="Public hostname"
              value={w.env?.ingress?.host ?? ""}
              onChange={(host) =>
                w.edit(`deploy.environments.${w.environment}.ingress`, {
                  ...w.env?.ingress,
                  host,
                })
              }
            />
            <TextField
              label="TLS issuer"
              value={w.env?.ingress?.tls ?? ""}
              onChange={(tls) =>
                w.edit(`deploy.environments.${w.environment}.ingress`, {
                  ...w.env?.ingress,
                  host: w.env?.ingress?.host ?? "",
                  tls,
                })
              }
            />
          </div>
        </Panel>
      </TabsContent>
      <TabsContent value="storage">
        <Panel
          title="Configuration persistence"
          description="Files are the default. Use SQLite locally or PostgreSQL to share deployment settings and history."
        >
          <div className="grid gap-4 sm:grid-cols-2">
            <SelectField
              label="Settings backend"
              value={backend}
              onChange={(value) => {
                setBackend(value);
                setReference(
                  value === "sqlite"
                    ? ".forge/deploy.db"
                    : value === "postgres"
                      ? "env:FORGE_DEPLOY_DATABASE_URL"
                      : "",
                );
              }}
              options={[
                { value: "files", label: "Project files (default)" },
                { value: "sqlite", label: "SQLite" },
                { value: "postgres", label: "PostgreSQL" },
              ]}
            />
            {backend !== "files" && (
              <TextField
                label="Store reference"
                value={reference}
                onChange={setReference}
                help={
                  backend === "sqlite"
                    ? "Use a project-local .forge database file."
                    : "Use env:NAME or file:path. Keep the database credential outside browser settings."
                }
              />
            )}
          </div>
          <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
            <p className="text-xs text-muted-foreground">
              Current authority: {w.project?.settings.store.backend}. Deployment
              settings and history move together.
            </p>
            <Button
              size="sm"
              disabled={w.busy || w.dirty}
              onClick={() =>
                void w.act(async () => {
                  if (!w.project) throw new Error("Project not loaded");
                  await w.api.request("store", {
                    expected: w.project.settings.hash,
                    backend,
                    reference,
                  });
                  await w.load();
                })
              }
            >
              <HardDrive />
              Use storage
            </Button>
          </div>
          <p className="mt-3 text-xs leading-relaxed text-muted-foreground">
            Project source and build settings remain files. A failed migration
            leaves the current authority selected; unavailable database storage
            reports an error.
          </p>
        </Panel>
      </TabsContent>
      <TabsContent value="secrets">
        <Panel
          title="Secret references"
          description="Configure how Forge resolves credentials. Credential values stay outside generated artifacts."
        >
          <div className="grid gap-4 sm:grid-cols-2">
            <SelectField
              label="Secret resolver"
              value={secrets.resolver ?? "env"}
              onChange={(resolver) =>
                w.edit("deploy.secrets", { ...secrets, resolver })
              }
              options={[
                { value: "env", label: "Process environment" },
                { value: "file", label: "Private project file" },
                { value: "kubernetes", label: "Kubernetes Secrets" },
              ]}
            />
            {secrets.resolver === "file" && (
              <TextField
                label="Private secret file"
                value={secrets.file ?? ".forge/secrets.env"}
                onChange={(file) =>
                  w.edit("deploy.secrets", { ...secrets, file })
                }
              />
            )}
          </div>
          <div className="mt-4 flex items-center gap-2 rounded border bg-muted/30 p-3 text-xs text-muted-foreground">
            <LockKeyhole className="size-4 shrink-0" />
            The workbench displays names and references. Create private
            credential values in your chosen resolver.
          </div>
        </Panel>
      </TabsContent>
    </Tabs>
  );
}
export function FilesView({ w }: { w: Workspace }) {
  const [path, setPath] = useState("");
  const artifacts = w.plan?.artifacts ?? {};
  const entries = Object.keys(artifacts);
  const active = entries.includes(path) ? path : entries[0];
  return (
    <>
      <Panel
        title="Generated artifacts"
        description={
          w.plan
            ? "Artifacts from the current immutable plan. Export writes the generated files with an ownership manifest."
            : "Build a plan to preview the actual provider output."
        }
        action={
          <IconButton
            label="Export artifacts"
            icon={<Download />}
            onClick={() => void w.exportPlan()}
            disabled={!w.plan || w.dirty || w.busy}
          />
        }
      >
        {active ? (
          <>
            <div className="mb-2 flex items-center justify-between gap-2">
              <NativeSelect
                aria-label="Artifact file"
                value={active}
                onChange={(e) => setPath(e.target.value)}
              >
                {entries.map((name) => (
                  <NativeSelectOption key={name} value={name}>
                    {name}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
              <CopyButton text={artifacts[active]} label="Copy artifact" />
            </div>
            <pre className="code-panel max-h-[28rem]">{artifacts[active]}</pre>
          </>
        ) : (
          <ZeroState
            title="No generated plan yet"
            body="Save your configuration and build a plan to inspect provider files, resource bindings and rollout operations."
            illustration={<FileCode2 className="size-6" />}
            action={
              <Button
                size="sm"
                onClick={() => void w.buildPlan()}
                disabled={w.busy}
              >
                <FileCode2 />
                Build plan
              </Button>
            }
          />
        )}
      </Panel>
      <Panel
        title="Configuration files"
        description="Deployment sections shown here are read from your selected authority. Structured edits preserve comments."
      >
        <Tabs defaultValue={w.project?.settings.files[0]?.path}>
          <TabsList className="max-w-full flex-wrap h-auto">
            {w.project?.settings.files.map((file) => (
              <TabsTrigger key={file.path} value={file.path}>
                {file.path}
              </TabsTrigger>
            ))}
          </TabsList>
          {w.project?.settings.files.map((file) => (
            <TabsContent key={file.path} value={file.path}>
              <div className="mb-1 flex items-center justify-between">
                <span className="font-mono text-[10px] text-muted-foreground">
                  {file.hash.slice(0, 12)}
                </span>
                <CopyButton label="Copy configuration" text={file.content} />
              </div>
              <pre className="code-panel max-h-80">{file.content}</pre>
            </TabsContent>
          ))}
        </Tabs>
      </Panel>
    </>
  );
}
export function Diagnostics({ diagnostics }: { diagnostics: Diagnostic[] }) {
  return (
    <div className="space-y-2">
      {diagnostics.map((d, i) => (
        <div
          key={i}
          className={`flex items-start gap-2 rounded border p-2.5 text-xs ${d.severity === "error" ? "border-destructive/25 bg-destructive/5" : "bg-muted/20"}`}
        >
          <ShieldCheck className="mt-0.5 size-3.5 shrink-0" />
          <div>
            <span className="font-medium">{d.code}</span>
            <p className="mt-1">{d.message}</p>
            {d.field && (
              <code className="mt-1 block text-[10px] text-muted-foreground">
                {d.field}
              </code>
            )}
            {d.fix && <p className="mt-1 text-muted-foreground">{d.fix}</p>}
          </div>
        </div>
      ))}
    </div>
  );
}
export function PreflightView({ w }: { w: Workspace }) {
  const [diagnostics, setDiagnostics] = useState<Diagnostic[]>();
  const [offline, setOffline] = useState(false);
  return (
    <Panel
      title="Deployment preflight"
      description="Check declared services, backend capabilities, delivery settings and target access."
      action={
        <IconButton
          label="Run preflight"
          icon={w.busy ? <Spinner /> : <ShieldCheck />}
          onClick={() =>
            void w.act(async () => {
              if (w.dirty) await w.save();
              setDiagnostics(
                await w.api.request<Diagnostic[]>("doctor", {
                  target: w.profile,
                  env: w.environment,
                  offline,
                }),
              );
            })
          }
          disabled={w.busy}
        />
      }
    >
      <label className="mb-4 flex items-center gap-2 text-xs">
        <Checkbox checked={offline} onCheckedChange={setOffline} />
        Offline configuration checks
      </label>
      {diagnostics ? (
        diagnostics.length ? (
          <Diagnostics diagnostics={diagnostics} />
        ) : (
          <div
            role="status"
            className="flex items-center gap-2 rounded border bg-success/5 p-3 text-sm"
          >
            <CheckCheck className="size-4" />
            {offline
              ? "Configuration checks passed"
              : "Preflight checks passed"}
          </div>
        )
      ) : (
        <ZeroState
          title="Run checks before deploying"
          body="Forge reports open decisions and unsupported resource or delivery choices with their configuration paths."
          illustration={<ShieldCheck className="size-6" />}
          action={
            <Button
              size="sm"
              onClick={() =>
                void w.act(async () => {
                  if (w.dirty) await w.save();
                  setDiagnostics(
                    await w.api.request<Diagnostic[]>("doctor", {
                      target: w.profile,
                      env: w.environment,
                      offline,
                    }),
                  );
                })
              }
            >
              Run checks
            </Button>
          }
        />
      )}
    </Panel>
  );
}
export function ReviewPane({ w }: { w: Workspace }) {
  const [tab, setTab] = useState("review");
  const plan = w.plan?.plan;
  const command = `forge deploy plan --target ${w.profile} --env ${w.environment}${w.selected.length ? ` --services ${w.selected.join(",")}` : ""} --output json --non-interactive\n${plan ? `forge deploy apply --plan ${plan.hash} --approve-plan ${plan.hash} --non-interactive --output json` : "forge deploy inspect --output json --non-interactive"}`;
  return (
    <Panel
      title="Deployment review"
      className="review-pane"
      action={
        <Badge variant={w.dirty ? "secondary" : "outline"}>
          {w.dirty ? "Draft changed" : plan ? "Planned" : "Saved"}
        </Badge>
      }
    >
      <Tabs value={tab} onValueChange={(value) => setTab(String(value))}>
        <TabsList className="w-full">
          <TabsTrigger value="review">Review</TabsTrigger>
          <TabsTrigger value="files">Files</TabsTrigger>
          <TabsTrigger value="cli">CLI & AI</TabsTrigger>
        </TabsList>
        <TabsContent value="review">
          <div className="space-y-3">
            <div className="grid grid-cols-2 gap-2 border-b pb-3">
              <div>
                <p className="text-lg font-medium">{w.selected.length}</p>
                <p className="text-[11px] text-muted-foreground">
                  Services selected
                </p>
              </div>
              <div>
                <p className="text-lg font-medium">
                  {Object.keys(w.draft?.resources ?? {}).length}
                </p>
                <p className="text-[11px] text-muted-foreground">
                  Declared resources
                </p>
              </div>
            </div>
            <div className="flex flex-wrap gap-1">
              {w.selected.map((service) => (
                <Badge
                  key={service}
                  variant="secondary"
                  className="text-[10px]"
                >
                  {service}
                </Badge>
              ))}
            </div>
            {plan ? (
              <>
                <div className="space-y-1.5">
                  {plan.operations.map((operation) => (
                    <div
                      key={operation.id}
                      className="flex items-start gap-2 text-xs"
                    >
                      <span className="mt-0.5 size-1.5 shrink-0 rounded-full bg-primary/50" />
                      <p>
                        <span className="text-muted-foreground">
                          {operation.kind}:
                        </span>{" "}
                        {operation.detail}
                      </p>
                      {operation.destructive && (
                        <Badge variant="destructive">Destructive</Badge>
                      )}
                    </div>
                  ))}
                </div>
                <div className="rounded border bg-muted/20 p-2">
                  <div className="flex items-center justify-between text-[10px] text-muted-foreground">
                    <span>Plan hash</span>
                    <CopyButton label="Copy plan hash" text={plan.hash} />
                  </div>
                  <code className="block break-all text-[10px] leading-relaxed">
                    {plan.hash}
                  </code>
                </div>
                <label className="flex items-start gap-2 text-xs leading-relaxed">
                  <Checkbox
                    aria-label="Approve this exact plan"
                    checked={w.approved}
                    disabled={
                      w.dirty ||
                      w.busy ||
                      !(w.canApply || w.canPublish) ||
                      plan.operations.some((o) => o.destructive)
                    }
                    onCheckedChange={w.setApproved}
                  />
                  <span>Approve this exact plan</span>
                </label>
                {plan.operations.some((o) => o.destructive) && (
                  <p className="text-xs text-destructive">
                    This plan contains destructive operations. Review and
                    approve it explicitly through the CLI.
                  </p>
                )}
                <div className="flex items-center gap-2">
                  <IconButton
                    label="Apply plan"
                    icon={<Rocket />}
                    onClick={() => void w.apply()}
                    disabled={!w.approved || w.dirty || w.busy || !w.canApply}
                    variant="default"
                  />
                  <IconButton
                    label="Export plan files"
                    icon={<Download />}
                    onClick={() => void w.exportPlan()}
                    disabled={w.dirty || w.busy}
                  />
                  <span className="text-xs text-muted-foreground">
                    {w.canApply
                      ? "Apply approved plan"
                      : w.providerLevel === "unavailable"
                        ? "Adapter unavailable"
                        : w.target?.release?.mode === "gitops"
                          ? "Sync through your controller"
                          : "Export and hand off"}
                  </span>
                </div>
              </>
            ) : (
              <div className="rounded border border-dashed p-3 text-xs leading-relaxed text-muted-foreground">
                Save your settings, then build a plan. Forge resolves service
                communication, data bindings and provider operations before
                approval.
              </div>
            )}
            {w.dirty && (
              <p className="text-xs text-warning">
                Configuration changed. Save and rebuild the plan before
                deployment.
              </p>
            )}
          </div>
        </TabsContent>
        <TabsContent value="files">
          <div className="space-y-2">
            {w.plan ? (
              Object.keys(w.plan.artifacts).map((path) => (
                <div key={path} className="flex items-center gap-2 text-xs">
                  <FileCode2 className="size-3.5" />
                  <code className="break-all">{path}</code>
                </div>
              ))
            ) : (
              <ZeroState
                title="Files appear after planning"
                body="The preview includes the provider artifacts generated from your selected services."
                illustration={<FileCode2 className="size-6" />}
              />
            )}
          </div>
        </TabsContent>
        <TabsContent value="cli">
          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-xs text-muted-foreground">
                Same configuration, same approval
              </span>
              <CopyButton label="Copy CLI commands" text={command} />
            </div>
            <pre className="code-panel whitespace-pre-wrap break-all text-[10px]">
              {command}
            </pre>
            <p className="text-xs leading-relaxed text-muted-foreground">
              Use <code>--output json --non-interactive</code> for an AI or CI
              workflow. Unresolved decisions fail with diagnostics; exact plan
              approval is required.
            </p>
          </div>
        </TabsContent>
      </Tabs>
    </Panel>
  );
}
export function ActivityView({ w }: { w: Workspace }) {
  const [proof, setProof] = useState<Lifecycle>();
  const [deleteData, setDeleteData] = useState(false);
  const [logs, setLogs] = useState("");
  const refresh = w.refresh;
  useEffect(() => {
    void refresh();
  }, [refresh]);
  const active = w.runs.filter(
    (run) => run.target === w.profile && run.environment === w.environment,
  );
  const progress = w.events.filter(
    (event) =>
      (!event.target || event.target === w.profile) &&
      (!event.environment || event.environment === w.environment),
  );
  return (
    <>
      <Panel
        title="Environment status"
        description="Status comes from the provider and recorded deployment history."
        action={
          <IconButton
            label="Refresh status"
            icon={<RefreshCw />}
            onClick={() => void w.act(w.refresh)}
            disabled={w.busy}
          />
        }
      >
        <div className="flex flex-wrap items-center gap-3">
          <Badge variant="outline">
            {w.status?.overall ?? w.history?.status ?? "Not observed"}
          </Badge>
          {(w.status?.failed_operation || w.history?.failed_operation) && (
            <p className="text-xs text-destructive">
              Failed operation:{" "}
              <code>
                {w.status?.failed_operation ?? w.history?.failed_operation}
              </code>
            </p>
          )}
        </div>
        {Object.entries(w.status?.services ?? {}).length > 0 && (
          <Table className="mt-3">
            <TableHeader>
              <TableRow>
                <TableHead>Service</TableHead>
                <TableHead>Ready</TableHead>
                <TableHead>State</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {Object.entries(w.status?.services ?? {}).map(([name, s]) => (
                <TableRow key={name}>
                  <TableCell>{name}</TableCell>
                  <TableCell>
                    {s.ready} / {s.desired}
                  </TableCell>
                  <TableCell>{s.message ?? "Observed"}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Panel>
      <Panel title="Deployment activity">
        {active.length || progress.length ? (
          <div className="space-y-3">
            {active.map((run) => (
              <div
                key={run.id}
                className="flex items-center justify-between gap-2 text-xs"
              >
                <span>
                  {run.action} · {run.status}
                </span>
                {run.status === "running" && (
                  <IconButton
                    label="Cancel deployment"
                    icon={<X />}
                    onClick={() =>
                      void w.act(() => w.api.request("cancel", { run: run.id }))
                    }
                    variant="destructive"
                  />
                )}
                {run.error && (
                  <span className="text-destructive">{run.error.message}</span>
                )}
              </div>
            ))}
            <div className="max-h-64 space-y-2 overflow-y-auto border-t pt-3">
              {progress.map((event, i) => (
                <div key={`${event.id}-${i}`} className="flex gap-2 text-xs">
                  <span className="min-w-16 text-muted-foreground">
                    {event.operation?.status ?? event.type}
                  </span>
                  <p className="min-w-0 break-words">
                    {event.operation?.op && <code>{event.operation.op}: </code>}
                    {event.operation?.message ??
                      event.error?.message ??
                      event.type}
                  </p>
                </div>
              ))}
            </div>
          </div>
        ) : (
          <ZeroState
            title="No deployment runs in this session"
            body="Build and approve a plan to start a run. Saved release history is shown below."
            illustration={<Activity className="size-6" />}
          />
        )}
      </Panel>
      <Panel
        title="Release history"
        action={
          <IconButton
            label="Review workload removal"
            icon={<Trash2 />}
            onClick={async () => {
              const preview = await w.inspectLifecycle(
                "destroy",
                undefined,
                deleteData,
              );
              if (preview) setProof(preview);
            }}
            disabled={w.busy || !w.canApply || w.dirty}
          />
        }
      >
        {w.history?.releases?.length ? (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Release</TableHead>
                <TableHead>Result</TableHead>
                <TableHead className="w-9" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {w.history.releases.map((release) => (
                <TableRow key={release.id}>
                  <TableCell>
                    <code className="text-xs">{release.id}</code>
                    <p className="mt-1 text-[10px] text-muted-foreground">
                      {new Date(release.applied_at).toLocaleString()}
                    </p>
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline">{release.status}</Badge>
                  </TableCell>
                  <TableCell>
                    <IconButton
                      label={`Review rollback to ${release.id}`}
                      icon={<Undo2 />}
                      onClick={async () => {
                        const preview = await w.inspectLifecycle(
                          "rollback",
                          release.id,
                        );
                        if (preview) setProof(preview);
                      }}
                      disabled={w.busy || w.dirty || !w.canApply}
                      variant="ghost"
                    />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        ) : (
          <ZeroState
            title="No recorded releases"
            body="Release history appears after a deployment reaches its recorded outcome."
            illustration={<Server className="size-6" />}
          />
        )}
        <label className="mt-3 flex items-start gap-2 text-xs">
          <Checkbox checked={deleteData} onCheckedChange={setDeleteData} />
          <span>Include persistent data in a removal preview</span>
        </label>
      </Panel>
      <Panel title="Service logs">
        <div className="flex flex-wrap items-center gap-2">
          {Object.keys(w.status?.services ?? w.draft?.services ?? {}).map(
            (service) => (
              <Button
                key={service}
                size="sm"
                variant="outline"
                onClick={() =>
                  void w.act(async () => {
                    const response = await fetch(
                      `/api/logs?target=${encodeURIComponent(w.profile)}&env=${encodeURIComponent(w.environment)}&service=${encodeURIComponent(service)}&tail=200`,
                      {
                        credentials: "same-origin",
                        headers: { "X-Forge-Workbench": "1" },
                      },
                    );
                    if (!response.ok)
                      throw new Error("Service logs could not be read");
                    setLogs(await response.text());
                  })
                }
              >
                {service}
              </Button>
            ),
          )}
        </div>
        {logs && <pre className="code-panel mt-3 max-h-72">{logs}</pre>}
      </Panel>
      <Dialog
        open={!!proof}
        onOpenChange={(open) => {
          if (!open) setProof(undefined);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {proof?.request.action === "destroy"
                ? "Remove recorded resources"
                : "Restore a recorded release"}
            </DialogTitle>
            <DialogDescription>
              Approval applies to this exact recorded environment and expires at{" "}
              {proof ? new Date(proof.expires).toLocaleTimeString() : ""}.
            </DialogDescription>
          </DialogHeader>
          {proof && (
            <>
              <div className="space-y-2 text-xs">
                <p>
                  Target: {proof.request.target} / {proof.request.env}
                </p>
                <p>
                  Recorded provider:{" "}
                  {proof.state.recorded_plan.target_spec.provider}
                </p>
                {proof.state.recorded_plan.target_spec.context && (
                  <p>
                    Context: {proof.state.recorded_plan.target_spec.context}
                  </p>
                )}
                {proof.state.recorded_plan.target_spec.namespace && (
                  <p>
                    Namespace: {proof.state.recorded_plan.target_spec.namespace}
                  </p>
                )}
                <p>
                  {proof.request.delete_data
                    ? "Persistent data is included."
                    : "Persistent data is retained."}
                </p>
                {proof.request.release && (
                  <p>Release: {proof.request.release}</p>
                )}
                <code className="block break-all text-[10px]">
                  {proof.hash}
                </code>
              </div>
              <DialogFooter>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setProof(undefined)}
                >
                  Cancel
                </Button>
                <Button
                  size="sm"
                  variant={
                    proof.request.action === "destroy"
                      ? "destructive"
                      : "default"
                  }
                  disabled={w.busy || w.dirty}
                  onClick={async () => {
                    if (await w.applyLifecycle(proof)) setProof(undefined);
                  }}
                >
                  Approve {proof.request.action}
                </Button>
              </DialogFooter>
            </>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}
