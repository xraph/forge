import { useState, type CSSProperties } from "react";
import {
  Boxes,
  Check,
  ChevronRight,
  Cloud,
  Container,
  Database,
  FileCode2,
  GitBranch,
  Layers3,
  Network,
  Plus,
  RefreshCw,
  Rocket,
  Save,
  Server,
  Settings2,
  ShieldCheck,
  Workflow,
  X,
} from "lucide-react";
import { TooltipProvider } from "@forge-go/dashboard-kit/components/tooltip";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarTrigger,
  useSidebar,
} from "@forge-go/dashboard-kit/components/sidebar";
import { apiClient, type API, type Target } from "./api";
import { useWorkbench, type Section, type Workspace } from "./controller";
import {
  Badge,
  Button,
  Card,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  IconButton,
  NativeSelect,
  NativeSelectOption,
  Panel,
  SelectField,
  Spinner,
  TextField,
  ThemePicker,
  ZeroState,
} from "./components";
import {
  ActivityView,
  ConnectionsView,
  EnvironmentView,
  FilesView,
  ImagesView,
  OverviewView,
  PreflightView,
  ResourceView,
  ReviewPane,
  ServicesView,
} from "./views";
const navigation: {
  id: Section;
  label: string;
  icon: typeof Boxes;
  group: string;
}[] = [
  { id: "overview", label: "Overview", icon: Layers3, group: "Project" },
  { id: "services", label: "Services", icon: Boxes, group: "Project" },
  {
    id: "resources",
    label: "Data & messaging",
    icon: Database,
    group: "Project",
  },
  { id: "connections", label: "Connections", icon: Network, group: "Project" },
  {
    id: "images",
    label: "Images & delivery",
    icon: GitBranch,
    group: "Configuration",
  },
  {
    id: "environment",
    label: "Environment & storage",
    icon: Settings2,
    group: "Configuration",
  },
  { id: "files", label: "Files & plan", icon: FileCode2, group: "Release" },
  { id: "preflight", label: "Preflight", icon: ShieldCheck, group: "Release" },
  { id: "activity", label: "Activity", icon: Workflow, group: "Release" },
];
export function Workbench({ api = apiClient }: { api?: API }) {
  const w = useWorkbench(api);
  return (
    <TooltipProvider delay={250}>
      <a className="skip-link" href="#main">
        Skip to deployment configuration
      </a>
      {!w.project ? (
        <div className="mx-auto max-w-xl p-6">
          <ZeroState
            title={
              w.error ? "Project could not load" : "Reading your Forge project"
            }
            body={
              w.error?.message ??
              "Loading deployment settings and application metadata."
            }
            illustration={w.error ? <Server className="size-6" /> : <Spinner />}
            action={
              w.error ? (
                <Button size="sm" onClick={() => void w.act(w.load)}>
                  Retry
                </Button>
              ) : undefined
            }
          />
        </div>
      ) : w.gate ? (
        <TargetGate w={w} />
      ) : (
        <Shell w={w} />
      )}
    </TooltipProvider>
  );
}
function ErrorNotice({ w }: { w: Workspace }) {
  if (!w.error) return null;
  return (
    <div
      role="alert"
      className="flex flex-wrap items-start gap-2 rounded-lg border border-destructive/30 bg-destructive/5 p-3 text-sm"
    >
      <X className="mt-0.5 size-4 shrink-0 text-destructive" />
      <div className="min-w-0 flex-1">
        <p>{w.error.message}</p>
        {w.error.diagnostics.map((d, i) => (
          <p key={i} className="mt-1 text-xs text-muted-foreground">
            {d.field && `${d.field}: `}
            {d.message}
            {d.fix && ` (${d.fix})`}
          </p>
        ))}
      </div>
      <IconButton
        label="Reload configuration"
        icon={<RefreshCw />}
        onClick={() => void w.act(w.load)}
        disabled={w.busy}
      />
      <IconButton
        label="Dismiss error"
        icon={<X />}
        onClick={() => w.setError(undefined)}
        variant="ghost"
      />
    </div>
  );
}
function TargetGate({ w }: { w: Workspace }) {
  const [creating, setCreating] = useState(false);
  const targets = Object.entries(w.draft?.targets ?? {});
  return (
    <div className="min-h-svh bg-background">
      <header className="flex h-14 items-center justify-between border-b px-5">
        <Brand />
        <div className="flex items-center gap-3">
          <Badge variant="secondary">Local session</Badge>
          <ThemePicker />
        </div>
      </header>
      <main id="main" className="mx-auto max-w-5xl space-y-5 px-4 py-8 sm:px-6">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <p className="mb-1 text-xs text-muted-foreground">
              {w.project?.name} / Deployment workbench
            </p>
            <h1 className="text-2xl font-semibold tracking-tight">
              Where should we deploy?
            </h1>
            <p className="mt-2 text-sm text-muted-foreground">
              Choose a saved target. Your environments, service scope and
              delivery settings stay with your project.
            </p>
          </div>
          <IconButton
            label="Create target"
            icon={<Plus />}
            onClick={() => setCreating(true)}
            disabled={w.busy || w.dirty || !w.draft}
          />
        </div>
        <ErrorNotice w={w} />
        {!w.draft ? (
          <InitialConfiguration w={w} />
        ) : targets.length ? (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {targets.map(([name, target]) => {
              const Icon =
                target.provider === "compose"
                  ? Container
                  : target.provider === "kubernetes"
                    ? Boxes
                    : Cloud;
              const envs = Object.entries(w.draft?.environments ?? {}).filter(
                ([, env]) => env.target === name,
              );
              const level =
                w.project?.providers.find((p) => p.name === target.provider)
                  ?.level ?? "unavailable";
              return (
                <Card
                  size="sm"
                  key={name}
                  className={
                    w.profile === name
                      ? "ring-2 ring-primary"
                      : "hover:ring-primary/40"
                  }
                >
                  <Button
                    variant="ghost"
                    aria-label={`Select ${name}`}
                    onClick={() => w.chooseProfile(name)}
                    className="h-auto min-h-40 flex-col items-stretch gap-4 whitespace-normal px-4 py-1 text-left"
                  >
                    <div className="flex items-center justify-between">
                      <div className="flex size-10 items-center justify-center rounded-xl border bg-background">
                        <Icon className="size-5" />
                      </div>
                      {w.profile === name ? (
                        <Check className="size-4 text-primary" />
                      ) : (
                        <Badge variant="outline">
                          {["apply", "live-qualified"].includes(level)
                            ? "Deploy"
                            : level === "unavailable"
                              ? "Unavailable"
                              : "Export"}
                        </Badge>
                      )}
                    </div>
                    <div>
                      <p className="text-base font-medium">{name}</p>
                      <p className="mt-1 text-xs text-muted-foreground">
                        {providerName(target.provider)}
                        {target.region
                          ? ` · ${target.region}`
                          : target.context
                            ? ` · ${target.context}`
                            : ""}
                      </p>
                    </div>
                    <div className="flex items-center justify-between text-xs text-muted-foreground">
                      <span>
                        {envs.length}{" "}
                        {envs.length === 1 ? "environment" : "environments"}
                      </span>
                      <ChevronRight className="size-4" />
                    </div>
                  </Button>
                </Card>
              );
            })}
          </div>
        ) : (
          <ZeroState
            title="Create your first deployment target"
            body="Choose a platform and save its settings to your project."
            illustration={<Rocket className="size-6" />}
            action={
              <Button size="sm" onClick={() => setCreating(true)}>
                <Plus />
                Create target
              </Button>
            }
          />
        )}
        <div className="flex flex-wrap items-center justify-between gap-2 border-t pt-4 text-xs text-muted-foreground">
          <span className="flex items-center gap-2">
            <FileCode2 className="size-3.5" />
            Configuration authority:{" "}
            {w.project?.settings.store.backend ?? "files"}
          </span>
          <code>forge deploy start</code>
        </div>
      </main>
      <TargetDialog w={w} open={creating} onClose={() => setCreating(false)} />
    </div>
  );
}
function Brand() {
  return (
    <div className="flex items-center gap-2">
      <div className="flex size-7 items-center justify-center rounded-md bg-primary text-primary-foreground">
        <Layers3 className="size-4" />
      </div>
      <span className="font-semibold tracking-tight">forge</span>
      <span className="hidden text-xs text-muted-foreground sm:inline">
        Deploy
      </span>
    </div>
  );
}
function Navigation({ w }: { w: Workspace }) {
  const sidebar = useSidebar();
  return (
    <nav aria-label="Deployment sections">
      {["Project", "Configuration", "Release"].map((group) => (
        <SidebarGroup key={group}>
          <SidebarGroupLabel>{group}</SidebarGroupLabel>
          <SidebarMenu>
            {navigation
              .filter((n) => n.group === group)
              .map((n) => (
                <SidebarMenuItem key={n.id}>
                  <SidebarMenuButton
                    aria-label={n.label}
                    isActive={w.section === n.id}
                    tooltip={n.label}
                    onClick={() => {
                      w.setSection(n.id);
                      sidebar.setOpenMobile(false);
                    }}
                  >
                    <n.icon />
                    <span>{n.label}</span>
                    {n.id === "services" && (
                      <span className="ml-auto text-xs text-muted-foreground">
                        {w.selected.length}
                      </span>
                    )}
                  </SidebarMenuButton>
                </SidebarMenuItem>
              ))}
          </SidebarMenu>
        </SidebarGroup>
      ))}
    </nav>
  );
}
function Shell({ w }: { w: Workspace }) {
  const [creatingEnvironment, setCreatingEnvironment] = useState(false);
  const envs = Object.entries(w.draft?.environments ?? {}).filter(
    ([, env]) => env.target === w.profile,
  );
  const active = navigation.find((n) => n.id === w.section);
  const running = w.runs.some((run) => run.status === "running");
  return (
    <SidebarProvider style={{ "--sidebar-width": "15rem" } as CSSProperties}>
      <Sidebar collapsible="icon">
        <SidebarHeader className="gap-3 border-b p-3">
          <Brand />
          <Button
            variant="outline"
            aria-label="Manage targets"
            onClick={() => w.setGate(true)}
            className="justify-between group-data-[collapsible=icon]:size-8 group-data-[collapsible=icon]:p-0"
          >
            <span className="flex min-w-0 items-center gap-2">
              <Server className="size-4 shrink-0" />
              <span className="truncate group-data-[collapsible=icon]:hidden">
                {w.profile}
              </span>
            </span>
            <ChevronRight className="size-3.5 group-data-[collapsible=icon]:hidden" />
          </Button>
        </SidebarHeader>
        <SidebarContent>
          <Navigation w={w} />
        </SidebarContent>
        <SidebarFooter className="border-t p-3">
          <div className="space-y-2 group-data-[collapsible=icon]:hidden">
            <div className="flex items-center gap-2 text-xs">
              <span className="size-1.5 rounded-full bg-success" />
              Local session
              <span className="ml-auto text-muted-foreground">
                {w.project?.settings.store.backend}
              </span>
            </div>
            <code className="block rounded border bg-background px-2 py-1.5 text-[11px]">
              forge deploy start
            </code>
            <p className="text-[11px] text-muted-foreground">
              Your files, CLI and page share one plan.
            </p>
          </div>
        </SidebarFooter>
      </Sidebar>
      <SidebarInset className="min-w-0">
        <header className="sticky top-0 z-20 flex min-h-12 flex-wrap items-center gap-2 border-b bg-background/95 px-3 backdrop-blur-sm sm:px-5">
          <SidebarTrigger aria-label="Toggle navigation" />
          <span className="mr-1 text-sm font-medium">{w.project?.name}</span>
          <ChevronRight className="size-3 text-muted-foreground" />
          <span className="text-xs text-muted-foreground">{active?.label}</span>
          <div className="ml-auto flex items-center gap-2">
            <Badge variant="outline">
              {providerName(w.target?.provider ?? "")}
            </Badge>
            <ThemePicker />
          </div>
        </header>
        <main id="main" className="min-w-0 space-y-4 p-3 sm:p-5">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h1 className="text-xl font-semibold tracking-tight">
                {active?.label}
              </h1>
              <p className="mt-1 text-xs text-muted-foreground">
                {w.profile}
                {w.target?.namespace ? ` / ${w.target.namespace}` : ""} ·{" "}
                {w.providerLevel === "unavailable"
                  ? "Provider adapter unavailable"
                  : w.canApply
                    ? "Deployment supported"
                    : "Artifact export supported"}
              </p>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <NativeSelect
                aria-label="Active environment"
                value={w.environment}
                onChange={(e) => w.chooseEnvironment(e.target.value)}
              >
                <NativeSelectOption value="" disabled>
                  Choose environment
                </NativeSelectOption>
                {envs.map(([name]) => (
                  <NativeSelectOption key={name} value={name}>
                    {name}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
              <IconButton
                label="Create environment"
                icon={<Plus />}
                onClick={() => setCreatingEnvironment(true)}
                disabled={w.busy || w.dirty}
              />
              <span className="mx-1 h-5 border-l" />
              <Badge variant={w.dirty ? "secondary" : "outline"}>
                {w.dirty ? "Unsaved" : "Saved"}
              </Badge>
              <IconButton
                label="Save configuration"
                icon={w.busy ? <Spinner /> : <Save />}
                onClick={() => void w.act(w.save)}
                disabled={w.busy || !w.dirty || running}
              />
              <IconButton
                label="Build plan"
                icon={<FileCode2 />}
                onClick={() => void w.buildPlan()}
                disabled={w.busy || !w.environment || running}
                variant="default"
              />
            </div>
          </div>
          <ErrorNotice w={w} />
          {w.notice && (
            <div
              role="status"
              className="flex items-center gap-2 text-xs text-muted-foreground"
            >
              <Check className="size-3.5" />
              {w.notice}
            </div>
          )}
          {!w.environment ? (
            <ZeroState
              title={`Create an environment for ${w.profile}`}
              body="An environment defines your service scope, resource placement and runtime settings."
              illustration={<Layers3 className="size-6" />}
              action={
                <Button size="sm" onClick={() => setCreatingEnvironment(true)}>
                  <Plus />
                  Create environment
                </Button>
              }
            />
          ) : (
            <div className="workspace-grid">
              <div className="min-w-0 space-y-4">
                {w.section === "overview" && <OverviewView w={w} />}{" "}
                {w.section === "services" && <ServicesView w={w} />}{" "}
                {w.section === "resources" && <ResourceView w={w} />}{" "}
                {w.section === "connections" && (
                  <ConnectionsView
                    key={`${w.project!.settings.hash}:${w.reloadVersion}`}
                    w={w}
                  />
                )}{" "}
                {w.section === "images" && <ImagesView w={w} />}{" "}
                {w.section === "environment" && <EnvironmentView w={w} />}{" "}
                {w.section === "files" && <FilesView w={w} />}{" "}
                {w.section === "preflight" && <PreflightView w={w} />}{" "}
                {w.section === "activity" && <ActivityView w={w} />}
              </div>
              <aside aria-label="Deployment review" className="min-w-0">
                <ReviewPane w={w} />
              </aside>
            </div>
          )}
        </main>
      </SidebarInset>
      <EnvironmentDialog
        w={w}
        open={creatingEnvironment}
        onClose={() => setCreatingEnvironment(false)}
      />
    </SidebarProvider>
  );
}
function TargetDialog({
  w,
  open,
  onClose,
}: {
  w: Workspace;
  open: boolean;
  onClose: () => void;
}) {
  const [name, setName] = useState("");
  const [provider, setProvider] = useState("compose");
  const [context, setContext] = useState("");
  const [namespace, setNamespace] = useState("");
  const [region, setRegion] = useState("");
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        if (!value) onClose();
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Create deployment target</DialogTitle>
          <DialogDescription>
            Save a named platform configuration to your project.
          </DialogDescription>
        </DialogHeader>
        <TextField
          label="Target name"
          value={name}
          onChange={setName}
          placeholder="staging"
        />
        <SelectField
          label="Provider"
          value={provider}
          onChange={setProvider}
          options={[
            { value: "compose", label: "Docker Compose" },
            { value: "kubernetes", label: "Kubernetes" },
            { value: "render", label: "Render" },
            { value: "digitalocean", label: "DigitalOcean App Platform" },
            { value: "vm", label: "Virtual machine handoff" },
            { value: "fly", label: "Fly.io portable handoff" },
            { value: "railway", label: "Railway portable handoff" },
            { value: "hosted", label: "Hosted control-plane contract" },
          ]}
        />
        {["vm", "fly", "railway", "hosted"].includes(provider) && (
          <p className="text-xs text-muted-foreground">
            {provider === "vm"
              ? "Export for an existing virtual machine with Docker Compose. No machine is created."
              : provider === "hosted"
                ? "Export workload contracts and vault references for import through your hosted authority."
                : "Export a portable Compose graph. Native platform configuration needs manual translation."}
          </p>
        )}
        {provider === "kubernetes" && (
          <>
            <TextField
              label="Kubernetes context"
              value={context}
              onChange={setContext}
            />
            <TextField
              label="Namespace"
              value={namespace}
              onChange={setNamespace}
            />
          </>
        )}
        {["render", "digitalocean"].includes(provider) && (
          <TextField label="Region" value={region} onChange={setRegion} />
        )}
        <DialogFooter>
          <Button size="sm" variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button
            size="sm"
            disabled={w.busy || !name}
            onClick={async () => {
              const value: Target = {
                provider,
                ...(["vm", "fly", "railway", "hosted"].includes(provider)
                  ? { build: { source: "existing", delivery: "registry" } }
                  : {}),
                ...(context ? { context } : {}),
                ...(namespace ? { namespace } : {}),
                ...(region ? { region } : {}),
              };
              if (await w.saveTarget(name, value)) onClose();
            }}
          >
            Save target
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
function EnvironmentDialog({
  w,
  open,
  onClose,
}: {
  w: Workspace;
  open: boolean;
  onClose: () => void;
}) {
  const [name, setName] = useState("");
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        if (!value) onClose();
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Create environment</DialogTitle>
          <DialogDescription>
            This environment will use {w.profile}. Configure resource placement
            before planning.
          </DialogDescription>
        </DialogHeader>
        <TextField
          label="Environment name"
          value={name}
          onChange={setName}
          placeholder="staging"
        />
        <DialogFooter>
          <Button size="sm" variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button
            size="sm"
            disabled={w.busy || !name}
            onClick={async () => {
              if (await w.saveEnvironment(name)) onClose();
            }}
          >
            Save environment
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
export function providerName(name: string) {
  return (
    (
      {
        compose: "Docker Compose",
        kubernetes: "Kubernetes",
        render: "Render",
        digitalocean: "DigitalOcean",
        vm: "Virtual machine",
        fly: "Fly.io handoff",
        railway: "Railway handoff",
        hosted: "Hosted contract",
      } as Record<string, string>
    )[name] ?? name
  );
}

function InitialConfiguration({ w }: { w: Workspace }) {
  const [answers, setAnswers] = useState<Record<string, string>>({});
  const decisions = w.project?.decisions ?? [];
  const resourceChoices = new Set(
    decisions.filter((d) => d.kind === "resource").map((d) => d.path),
  );
  const activeDecisions = decisions.filter((decision) => {
    const resource = decision.path.match(/^(deploy\.resources\.[^.]+)\./)?.[1];
    return (
      !resource ||
      !resourceChoices.has(resource) ||
      answers[resource] === "include"
    );
  });
  const remaining = activeDecisions.filter((decision) => {
    const answer = answers[decision.path];
    return (
      !answer ||
      (decision.options?.length && !decision.options.includes(answer))
    );
  }).length;
  return (
    <Panel
      title="Discover deployment settings"
      description="Review the project's declared applications and answer open deployment choices before creating targets."
    >
      <div className="space-y-4">
        <div className="grid gap-4 sm:grid-cols-2">
          {activeDecisions.map((decision) => (
            <div key={decision.path} className="min-w-0 space-y-1">
              {decision.options?.length ? (
                <SelectField
                  label={decision.question || decision.path}
                  value={answers[decision.path] ?? ""}
                  onChange={(value) =>
                    setAnswers({ ...answers, [decision.path]: value })
                  }
                  options={[
                    { value: "", label: "Choose an answer" },
                    ...decision.options.map((value) => ({
                      value,
                      label:
                        value === "include"
                          ? "Include resource"
                          : value === "skip"
                            ? "Skip resource"
                            : value,
                    })),
                  ]}
                />
              ) : (
                <TextField
                  label={decision.question || decision.path}
                  value={answers[decision.path] ?? ""}
                  onChange={(value) =>
                    setAnswers({ ...answers, [decision.path]: value })
                  }
                />
              )}
              <p className="break-all text-[11px] text-muted-foreground">
                {decision.source} · {decision.confidence}
              </p>
            </div>
          ))}
        </div>
        {!decisions.length && (
          <p className="text-sm text-muted-foreground">
            Forge found {w.project?.apps?.length ?? 0} declared applications.
            Generate the initial configuration, then review its services and
            resource placement.
          </p>
        )}
        <div className="flex flex-wrap items-center gap-3">
          <Button
            size="sm"
            disabled={w.busy || remaining > 0}
            onClick={() =>
              void w.act(async () => {
                await w.api.request("init", {
                  answers: Object.fromEntries(
                    activeDecisions.map((d) => [d.path, answers[d.path]]),
                  ),
                  force: false,
                });
                await w.load();
              })
            }
          >
            <FileCode2 />
            Initialize deployment
          </Button>
          {remaining > 0 && (
            <p role="status" className="text-xs text-muted-foreground">
              Answer {remaining} remaining{" "}
              {remaining === 1 ? "choice" : "choices"} to initialize.
            </p>
          )}
        </div>
      </div>
    </Panel>
  );
}
