import { useCallback, useEffect, useRef, useState } from "react";
import {
  RequestError,
  type API,
  type Deploy,
  type Environment,
  type History,
  type Lifecycle,
  type Op,
  type Planned,
  type Progress,
  type Project,
  type Run,
  type Settings,
  type Status,
  type Target,
} from "./api";
import { persistPreference, preference } from "./components";
export type Section =
  | "overview"
  | "services"
  | "resources"
  | "connections"
  | "images"
  | "environment"
  | "files"
  | "preflight"
  | "activity";
export type DraftDeploy = Deploy & {
  spec?: string;
  environment_files?: Record<string, string>;
};
function applyOp(deploy: DraftDeploy, op: Op) {
  const draft = structuredClone(deploy);
  const keys = op.path.replace(/^deploy\./, "").split(".");
  let node = draft as unknown as Record<string, unknown>;
  for (const key of keys.slice(0, -1)) {
    node[key] ??= {};
    node = node[key] as Record<string, unknown>;
  }
  if (op.delete) delete node[keys.at(-1)!];
  else node[keys.at(-1)!] = op.value;
  return draft;
}
function owner(path: string, settings: Settings) {
  const parts = path.split(".");
  const deploy = settings.deploy as DraftDeploy | null;
  if (
    ["services", "resources", "connections"].includes(parts[1]) &&
    deploy?.spec
  )
    return deploy.spec;
  if (parts[1] === "environments" && deploy?.environment_files?.[parts[2]])
    return deploy.environment_files[parts[2]];
  return ".forge.yml";
}
function normalizeProject(p: Project): Project {
  const deploy = p.settings.deploy;
  return {
    ...p,
    apps: p.apps ?? [],
    suggestions: p.suggestions ?? [],
    diagnostics: p.diagnostics ?? [],
    providers: p.providers ?? [],
    settings: {
      ...p.settings,
      files: p.settings.files ?? [],
      deploy: deploy
        ? {
            ...deploy,
            services: deploy.services ?? {},
            targets: deploy.targets ?? {},
            environments: deploy.environments ?? {},
          }
        : null,
    },
  };
}
export function useWorkbench(api: API) {
  const [project, setProject] = useState<Project>();
  const [draft, setDraft] = useState<DraftDeploy>();
  const [profile, setProfile] = useState("");
  const [environment, setEnvironment] = useState("");
  const [gate, setGate] = useState(true);
  const [reloadVersion, setReloadVersion] = useState(0);
  const [section, setSection] = useState<Section>("overview");
  const [pending, setPending] = useState<Record<string, Op>>({});
  const [error, setError] = useState<RequestError>();
  const statusError = useRef<RequestError | undefined>(undefined);
  const [busy, setBusy] = useState(false);
  const busyRef = useRef(false);
  const refreshGeneration = useRef(0);
  const [plan, setPlan] = useState<Planned>();
  const [approved, setApproved] = useState(false);
  const [status, setStatus] = useState<Status>();
  const [history, setHistory] = useState<History>();
  const [runs, setRuns] = useState<Run[]>([]);
  const [events, setEvents] = useState<Progress[]>([]);
  const [notice, setNotice] = useState("");
  const report = useCallback((error: unknown) => {
    const failure =
      error instanceof RequestError
        ? error
        : new RequestError(
            error instanceof Error ? error.message : "Request failed",
            2,
          );
    if (failure.code === 6) setApproved(false);
    setError(failure);
    return failure;
  }, []);
  const load = useCallback(
    async (signal?: AbortSignal) => {
      refreshGeneration.current++;
      const p = normalizeProject(
        await api.request<Project>("project", undefined, signal),
      );
      setProject(p);
      setReloadVersion((version) => version + 1);
      setDraft(
        p.settings.deploy ? structuredClone(p.settings.deploy) : undefined,
      );
      setPending({});
      setApproved(false);
      setPlan(undefined);
      if (profile && !p.settings.deploy?.targets[profile]) {
        setProfile("");
        setEnvironment("");
        setGate(true);
        setStatus(undefined);
        setHistory(undefined);
      } else if (
        profile &&
        p.settings.deploy?.environments[environment]?.target !== profile
      ) {
        const envs = Object.entries(
          p.settings.deploy?.environments ?? {},
        ).filter(([, env]) => env.target === profile);
        setEnvironment(envs[0]?.[0] ?? "");
        setStatus(undefined);
        setHistory(undefined);
      }
      return p;
    },
    [api, profile, environment],
  );
  useEffect(() => {
    const abort = new AbortController();
    api
      .request<Project>("project", undefined, abort.signal)
      .then((raw) => {
        if (abort.signal.aborted) return;
        const p = normalizeProject(raw);
        setProject(p);
        setDraft(
          p.settings.deploy ? structuredClone(p.settings.deploy) : undefined,
        );
        setPending({});
        setApproved(false);
        setPlan(undefined);
        const saved = preference(`forge-deploy:${p.name}:profile`);
        if (saved && p.settings.deploy?.targets[saved]) {
          setProfile(saved);
          const envs = Object.entries(p.settings.deploy.environments).filter(
            ([, value]) => value.target === saved,
          );
          const savedEnv = preference(
            `forge-deploy:${p.name}:${saved}:environment`,
          );
          setEnvironment(
            envs.find(([key]) => key === savedEnv)?.[0] ?? envs[0]?.[0] ?? "",
          );
          setGate(false);
        }
      })
      .catch((e) => {
        if (!abort.signal.aborted) report(e);
      });
    return () => abort.abort();
  }, [api, report]);
  const act = useCallback(
    async <T>(fn: () => Promise<T>) => {
      if (busyRef.current) return undefined;
      busyRef.current = true;
      setBusy(true);
      setError(undefined);
      try {
        return await fn();
      } catch (e) {
        report(e);
        return undefined;
      } finally {
        busyRef.current = false;
        setBusy(false);
      }
    },
    [report],
  );
  const dirty = Object.keys(pending).length > 0;
  const edit = (path: string, value: unknown, remove = false) => {
    if (!draft || !project || busyRef.current) return;
    const op: Op = remove ? { path, delete: true } : { path, value };
    const first = Object.values(pending)[0];
    if (
      first &&
      owner(first.path, project.settings) !== owner(path, project.settings)
    ) {
      report(
        new RequestError(
          `Save your edits to ${owner(first.path, project.settings)} before editing another configuration file.`,
          6,
        ),
      );
      return;
    }
    setDraft(applyOp(draft, op));
    setPending({ ...pending, [path]: op });
    setApproved(false);
    setNotice("");
  };
  const save = async () => {
    if (!project) return false;
    if (!dirty) return true;
    const settings = await api.request<Settings>("files", {
      expected: project.settings.hash,
      ops: Object.values(pending),
    });
    setProject({ ...project, settings });
    setDraft(settings.deploy ? structuredClone(settings.deploy) : undefined);
    setPending({});
    setApproved(false);
    setPlan(undefined);
    setNotice("Configuration saved");
    return true;
  };
  const chooseProfile = (name: string) => {
    if (busyRef.current) return;
    if (dirty) {
      report(
        new RequestError(
          "Save or discard your draft before switching deployment targets.",
          6,
        ),
      );
      return;
    }
    const envs = Object.entries(draft?.environments ?? {}).filter(
      ([, env]) => env.target === name,
    );
    const saved = project
      ? preference(`forge-deploy:${project.name}:${name}:environment`)
      : null;
    const env = envs.find(([key]) => key === saved)?.[0] ?? envs[0]?.[0] ?? "";
    refreshGeneration.current++;
    setProfile(name);
    setEnvironment(env);
    setGate(false);
    setPlan(undefined);
    setApproved(false);
    setStatus(undefined);
    setHistory(undefined);
    if (project)
      persistPreference(`forge-deploy:${project.name}:profile`, name);
  };
  const chooseEnvironment = (name: string) => {
    if (busyRef.current) return;
    if (dirty) {
      report(
        new RequestError(
          "Save or discard your draft before switching environments.",
          6,
        ),
      );
      return;
    }
    refreshGeneration.current++;
    setEnvironment(name);
    setPlan(undefined);
    setApproved(false);
    setStatus(undefined);
    setHistory(undefined);
    if (project)
      persistPreference(
        `forge-deploy:${project.name}:${profile}:environment`,
        name,
      );
  };
  const refresh = useCallback(async () => {
    const generation = ++refreshGeneration.current;
    if (!profile || !environment) return;
    const query = `?target=${encodeURIComponent(profile)}&env=${encodeURIComponent(environment)}`;
    const settled = await Promise.allSettled([
      api.request<Status>("status" + query),
      api.request<History>("history" + query),
      api.request<Run[]>("runs"),
    ]);
    if (generation !== refreshGeneration.current) return;
    if (settled[0].status === "fulfilled") {
      setStatus(settled[0].value);
      const recovered = statusError.current;
      setError((current) => (current === recovered ? undefined : current));
      statusError.current = undefined;
    } else statusError.current = report(settled[0].reason);
    if (settled[1].status === "fulfilled") setHistory(settled[1].value);
    if (settled[2].status === "fulfilled") setRuns(settled[2].value);
  }, [api, profile, environment, report]);
  useEffect(() => {
    const abort = new AbortController();
    api
      .events((event) => {
        setEvents((existing) =>
          event.id > 0 && existing.some((item) => item.id === event.id)
            ? existing
            : [...existing, event].slice(-100),
        );
        if (
          event.type === "reset" ||
          (["completed", "failed", "cancelled"].includes(event.type) &&
            event.target === profile &&
            event.environment === environment)
        ) {
          void refresh();
        }
        if (["failed", "cancelled"].includes(event.type)) setApproved(false);
      }, abort.signal)
      .catch((e) => {
        if (!abort.signal.aborted) report(e);
      });
    return () => abort.abort();
  }, [api, profile, environment, refresh, report]);
  const env = draft?.environments[environment];
  const target = draft?.targets[profile];
  const selected = target?.resource_only
    ? []
    : (env?.services ?? Object.keys(draft?.services ?? {}));
  const providerLevel =
    project?.providers.find((p) => p.name === target?.provider)?.level ??
    "unavailable";
  const canApply = ["apply", "live-qualified"].includes(providerLevel);
  const buildPlan = () =>
    act(async () => {
      if (!environment)
        throw new Error("Create an environment for this target first");
      if (!(await save())) return;
      const result = await api.request<Planned>("plan", {
        target: profile,
        env: environment,
        services: selected,
      });
      setPlan(result);
      setApproved(false);
      setSection("files");
      setNotice("Plan ready for review");
    });
  const apply = () =>
    act(async () => {
      if (!plan || !approved || dirty)
        throw new Error("Review and approve the current plan");
      const run = await api.request<Run>("apply", {
        hash: plan.plan.hash,
        approval: plan.plan.hash,
        allow_destructive: false,
      });
      setRuns((existing) => [
        ...existing.filter((item) => item.id !== run.id),
        run,
      ]);
      setApproved(false);
      setSection("activity");
      setNotice("Deployment started");
    });
  const exportPlan = () =>
    act(async () => {
      if (!plan || dirty) throw new Error("Build a current plan first");
      const result = await api.request<{
        Written?: string[];
        written?: string[];
      }>("export", { hash: plan.plan.hash });
      setNotice(
        `Artifacts exported (${(result.written ?? result.Written ?? []).length} files)`,
      );
    });
  const saveTarget = (name: string, value: Target) =>
    act(async () => {
      if (!project) throw new Error("Project is not loaded");
      if (dirty) throw new Error("Save your draft before creating a target");
      if (!/^[a-z][a-z0-9-]{0,62}$/.test(name) || draft?.targets[name])
        throw new Error("Choose a unique lowercase target name");
      await api.request<Settings>("files", {
        expected: project.settings.hash,
        ops: [{ path: `deploy.targets.${name}`, value }],
      });
      await load();
      setNotice(`Target ${name} saved`);
      return true;
    });
  const saveEnvironment = (name: string) =>
    act(async () => {
      if (!project || !draft) throw new Error("Project is not loaded");
      if (dirty) throw new Error("Save your draft first");
      if (!/^[a-z][a-z0-9-]{0,62}$/.test(name) || draft.environments[name])
        throw new Error("Choose a unique lowercase environment name");
      const value: Environment = { target: profile };
      await api.request<Settings>("files", {
        expected: project.settings.hash,
        ops: [{ path: `deploy.environments.${name}`, value }],
      });
      await load();
      setEnvironment(name);
      persistPreference(
        `forge-deploy:${project.name}:${profile}:environment`,
        name,
      );
      return true;
    });
  const inspectLifecycle = (
    action: string,
    release?: string,
    deleteData = false,
  ) =>
    act(() =>
      api.request<Lifecycle>("lifecycle/plan", {
        action,
        target: profile,
        env: environment,
        ...(release ? { release } : {}),
        ...(deleteData ? { delete_data: true } : {}),
      }),
    );
  const applyLifecycle = (proof: Lifecycle) =>
    act(async () => {
      const run = await api.request<Run>("lifecycle/apply", {
        hash: proof.hash,
        approval: proof.hash,
      });
      setRuns((old) => [...old, run]);
      setSection("activity");
      setNotice(`${run.action} started`);
      return true;
    });
  return {
    api,
    reloadVersion,
    project,
    draft,
    profile,
    environment,
    env,
    target,
    gate,
    setGate,
    section,
    setSection,
    pending,
    dirty,
    error,
    setError,
    report,
    busy,
    plan,
    approved,
    setApproved,
    status,
    history,
    runs,
    events,
    notice,
    selected,
    providerLevel,
    canApply,
    load,
    act,
    edit,
    save,
    chooseProfile,
    chooseEnvironment,
    refresh,
    buildPlan,
    apply,
    exportPlan,
    saveTarget,
    saveEnvironment,
    inspectLifecycle,
    applyLifecycle,
  };
}
export type Workspace = ReturnType<typeof useWorkbench>;
