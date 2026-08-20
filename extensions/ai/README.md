# AI (removed)

This extension is gone. It was a thin wrapper around
[ai-sdk](https://github.com/xraph/ai-sdk) that did DI registration and little
else, and it has been replaced by a family of focused projects, each in its own
repo shipping its own Forge extension.

| What you used it for | Use now |
| --- | --- |
| LLM abstraction, agent orchestration | [Cortex](https://github.com/xraph/cortex) |
| Guardrails, content filtering | [Shield](https://github.com/xraph/shield) |
| Monitoring and observability | [Sentinel](https://github.com/xraph/sentinel) |
| RAG pipelines, workflow orchestration | [Weave](https://github.com/xraph/weave) |
| Model hub, inference management | [Nexus](https://github.com/xraph/nexus) |

## Moving over

Pick the ones you actually need rather than reaching for all five. Most
applications that used this extension wanted Cortex alone.

```bash
go get github.com/xraph/cortex
```

Each project registers the same way every other Forge extension does, so the
shape of your app config does not change. What changes is that the services you
resolve from the container come from the project that owns them, rather than
from one wrapper that fronted all of it.

If you were calling into `ai-sdk` directly through this extension's DI keys,
you can keep doing that. The SDK did not move.
