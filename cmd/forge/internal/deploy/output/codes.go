package output

const (
	// CodeVersionMissing identifies `deploy:` exists without `version: 2`..
	CodeVersionMissing = "DEPLOY_VERSION_MISSING"
	// CodeVersionUnsupported identifies `version` is not 2..
	CodeVersionUnsupported = "DEPLOY_VERSION_UNSUPPORTED"
	// CodeUnknownKey identifies a key the schema does not define..
	CodeUnknownKey = "DEPLOY_UNKNOWN_KEY"
	// CodeDuplicateName identifies two services, resources or targets share a name..
	CodeDuplicateName = "DEPLOY_DUPLICATE_NAME"
	// CodeNameInvalid identifies a service or resource name is not DNS-safe..
	CodeNameInvalid = "DEPLOY_NAME_INVALID"
	// CodeAppUnknown identifies `service.app` names no buildable app..
	CodeAppUnknown = "DEPLOY_APP_UNKNOWN"
	// CodePortRequired identifies a `web` or `gateway` service has no port..
	CodePortRequired = "DEPLOY_PORT_REQUIRED"
	// CodePortForbidden identifies a `worker`, `job` or `cron` declares a port..
	CodePortForbidden = "DEPLOY_PORT_FORBIDDEN"
	// CodeScheduleRequired identifies a `cron` has no `schedule`..
	CodeScheduleRequired = "DEPLOY_SCHEDULE_REQUIRED"
	// CodeBindingResourceUnknown identifies a binding names a resource that does not exist..
	CodeBindingResourceUnknown = "DEPLOY_BINDING_RESOURCE_UNKNOWN"
	// CodeBindingTypeMismatch identifies the extension's descriptor does not accept the resource type..
	CodeBindingTypeMismatch = "DEPLOY_BINDING_TYPE_MISMATCH"
	// CodeBindingExtensionUnknown identifies no descriptor for the extension..
	CodeBindingExtensionUnknown = "DEPLOY_BINDING_EXTENSION_UNKNOWN"
	// CodeCallUnknown identifies `calls` names a service that does not exist..
	CodeCallUnknown = "DEPLOY_CALL_UNKNOWN"
	// CodeTargetUnknown identifies an environment or flag names a target that does not exist..
	CodeTargetUnknown = "DEPLOY_TARGET_UNKNOWN"
	// CodeEnvUnknown identifies a flag names an environment that does not exist..
	CodeEnvUnknown = "DEPLOY_ENV_UNKNOWN"
	// CodeLifecycleUnset identifies a resource has no lifecycle for the selected environment..
	CodeLifecycleUnset = "DEPLOY_LIFECYCLE_UNSET"
	// CodeLifecycleUnsupported identifies the target cannot host that lifecycle for that type..
	CodeLifecycleUnsupported = "DEPLOY_LIFECYCLE_UNSUPPORTED"
	// CodeSecretRequired identifies `lifecycle: external` without `secret`..
	CodeSecretRequired = "DEPLOY_SECRET_REQUIRED"
	// CodeSecretUnresolved identifies the resolver cannot find the named secret..
	CodeSecretUnresolved = "DEPLOY_SECRET_UNRESOLVED"
	// CodeMigrationOwnerConflict identifies two services claim migrations for one resource..
	CodeMigrationOwnerConflict = "DEPLOY_MIGRATION_OWNER_CONFLICT"
	// CodeSpecKeyConflict identifies a key appears inline and in the split file..
	CodeSpecKeyConflict = "DEPLOY_SPEC_KEY_CONFLICT"
	// CodeConfigAmbiguous identifies both `.forge.yml` and `.forge.yaml` exist..
	CodeConfigAmbiguous = "DEPLOY_CONFIG_AMBIGUOUS"
	// CodeConfigInvalid identifies the YAML does not parse..
	CodeConfigInvalid = "DEPLOY_CONFIG_INVALID"
	// CodeHealthPathUnverified identifies a health path the app does not register..
	CodeHealthPathUnverified = "DEPLOY_HEALTH_PATH_UNVERIFIED"
	// CodeContainerInProduction identifies a `container` lifecycle in a non-dev environment..
	CodeContainerInProduction = "DEPLOY_CONTAINER_IN_PRODUCTION"
	// CodeBackupRequired identifies `container` in production without `backup`..
	CodeBackupRequired = "DEPLOY_BACKUP_REQUIRED"
	// CodeDecisionOpen identifies a suggestion needs an answer before planning..
	CodeDecisionOpen = "DEPLOY_DECISION_OPEN"
	// CodeToolMissing identifies docker, kubectl, doctl or render not on PATH..
	CodeToolMissing = "DEPLOY_TOOL_MISSING"
	// CodeContextUnreachable identifies the Kubernetes context or Docker daemon does not answer..
	CodeContextUnreachable = "DEPLOY_CONTEXT_UNREACHABLE"
	// CodePrereqMissing identifies a controller, CRD or issuer the plan needs is absent..
	CodePrereqMissing = "DEPLOY_PREREQ_MISSING"
	// CodePlanStale identifies inputs changed since the plan was written..
	CodePlanStale = "DEPLOY_PLAN_STALE"
	// CodePlanHashMismatch identifies `--approve-plan` does not match..
	CodePlanHashMismatch = "DEPLOY_PLAN_HASH_MISMATCH"
	// CodeLocked identifies another apply holds the lock..
	CodeLocked = "DEPLOY_LOCKED"
	// CodeDrift identifies remote state differs from the plan's snapshot..
	CodeDrift = "DEPLOY_DRIFT"
	// CodeDestructiveBlocked identifies an operation would delete data without `--allow-destructive`..
	CodeDestructiveBlocked = "DEPLOY_DESTRUCTIVE_BLOCKED"
	// CodeUnsupportedCommand identifies the command or capability is not available on this adapter..
	CodeUnsupportedCommand = "DEPLOY_UNSUPPORTED_COMMAND"
	// CodeOverlayFallback identifies the project's forge version predates `FORGE_CONFIG_OVERLAY`..
	CodeOverlayFallback = "DEPLOY_OVERLAY_FALLBACK"
	// CodeDescriptorSchema identifies a descriptor declares a schema this CLI does not read..
	CodeDescriptorSchema = "DEPLOY_DESCRIPTOR_SCHEMA"
	// CodeDescriptorInvalid identifies a descriptor file does not parse..
	CodeDescriptorInvalid = "DEPLOY_DESCRIPTOR_INVALID"
	// CodeAccess identifies a resolver or tool could not be reached..
	CodeAccess = "DEPLOY_ACCESS"
)
