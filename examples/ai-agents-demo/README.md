# AI Agents Demo

Demonstrates the Forge v2 AI Extension's agent storage and management features.

## Features Demonstrated

### 🤖 Agent Management
- **Dynamic Agent Creation** - Create agents at runtime from definitions
- **Agent Storage** - Persist agents with pluggable storage (Memory, SQL)
- **Agent CRUD** - Create, Read, Update, Delete agents
- **Agent Execution** - Execute agent tasks

### 🏢 Agent Teams
- **Team Creation** - Group agents into teams
- **Sequential Execution** - Agents process tasks in order
- **Parallel Execution** - Agents work simultaneously
- **Collaborative Execution** - Agents share context

### 💾 Storage Options
- **Memory Store** - Fast, ephemeral (default)
- **SQL Store** - Persistent with any SQL database
- **Custom Store** - Implement `AgentStore` interface

## Usage

### Run the Demo

```bash
cd v2/examples/ai-agents-demo
go run main.go
```

### Storage Options

**Memory Store (ephemeral):**
```go
store := stores.NewMemoryAgentStore()
// Agents lost on restart
```

**SQL Store (persistent):**
```go
db, _ := sql.Open("sqlite3", "agents.db")
store := stores.NewSQLAgentStore(db, "agents", logger)
// Agents persist across restarts
```

### Creating Agents

```go
// Create agent definition
def := &ai.AgentDefinition{
    ID:           "my-agent-001",
    Name:         "My Agent",
    Type:         "assistant",
    SystemPrompt: "You are a helpful assistant",
    Model:        "gpt-4",
    Provider:     "openai",
}

// Create and register agent
agent, err := aiManager.CreateAgent(ctx, def)
```

### Creating Teams

```go
// Create team
team := ai.NewAgentTeam("team-001", "My Team", logger)

// Add agents
team.AddAgent(agent1)
team.AddAgent(agent2)

// Register team
aiManager.RegisterTeam(team)

// Execute team task
output, err := team.Execute(ctx, input)
```

### Execution Modes

**Sequential:**
```go
// Agents run one after another, output passed between them
output, err := team.Execute(ctx, input)
```

**Parallel:**
```go
// All agents run simultaneously with same input
outputs, err := team.ExecuteParallel(ctx, input)
```

**Collaborative:**
```go
// Agents share context and communicate
output, err := team.ExecuteCollaborative(ctx, input)
```

## REST API

Register the `AgentController` to enable REST API:

```go
import "github.com/xraph/forge/extensions/ai"

// Create controller
controller := ai.NewAgentController(app.Container())

// Register routes
router.Group("/api", func(r forge.Router) {
    controller.Routes(r)
})
```

### API Endpoints

**Agent CRUD:**
- `POST /agents` - Create agent
- `GET /agents` - List agents
- `GET /agents/:id` - Get agent
- `PUT /agents/:id` - Update agent
- `DELETE /agents/:id` - Delete agent

**Agent Execution:**
- `POST /agents/:id/execute` - Execute agent task
- `POST /agents/:id/chat` - Chat with agent
- `GET /agents/:id/history` - Get execution history

**Team Management:**
- `POST /teams` - Create team
- `GET /teams` - List teams
- `GET /teams/:id` - Get team
- `POST /teams/:id/agents/:agentId` - Add agent to team
- `DELETE /teams/:id/agents/:agentId` - Remove agent from team
- `POST /teams/:id/execute` - Execute team task
- `DELETE /teams/:id` - Delete team

### API Examples

**Create Agent:**
```bash
curl -X POST http://localhost:8080/api/agents \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Code Reviewer",
    "type": "code_reviewer",
    "system_prompt": "You are an expert code reviewer",
    "model": "gpt-4"
  }'
```

**Execute Agent:**
```bash
curl -X POST http://localhost:8080/api/agents/agent-123/execute \
  -H "Content-Type: application/json" \
  -d '{
    "type": "review",
    "data": "func main() { ... }"
  }'
```

**Create Team:**
```bash
curl -X POST http://localhost:8080/api/teams \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Dev Team",
    "agent_ids": ["agent-1", "agent-2"]
  }'
```

**Execute Team:**
```bash
curl -X POST http://localhost:8080/api/teams/team-123/execute \
  -H "Content-Type: application/json" \
  -d '{
    "type": "feature_development",
    "mode": "sequential",
    "data": {"feature": "auth"}
  }'
```

## Custom Storage

Implement the `AgentStore` interface for any backend:

```go
type MyCustomStore struct {
    // Your storage backend
}

func (s *MyCustomStore) Create(ctx context.Context, agent *ai.AgentDefinition) error {
    // Your implementation
}

func (s *MyCustomStore) Get(ctx context.Context, id string) (*ai.AgentDefinition, error) {
    // Your implementation
}

// ... implement other methods
```

Register your store:
```go
store := &MyCustomStore{}
app.Container().Register("agentStore", func(c forge.Container) (any, error) {
    return store, nil
})
```

## Database Schema

SQL stores use a simple single-table schema:

```sql
CREATE TABLE agents (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    data JSONB NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
```

All agent details stored in `data` JSONB column - no fixed schema!

## Configuration

```yaml
ai:
  enable_llm: true
  enable_agents: true
  enable_coordination: true
```

## Next Steps

1. **Set LLM API Keys** - Configure OpenAI/Anthropic/etc API keys
2. **Create Agent Templates** - Define reusable agent configurations
3. **Implement Custom Agents** - Extend beyond LLM-based agents
4. **Set Up REST API** - Enable HTTP management interface
5. **Add Monitoring** - Track agent performance and usage

## Learn More

- [Agent Storage Guide](../../extensions/ai/_impl_docs/AGENT_STORAGE_GUIDE.md)
- [AI Extension Documentation](../../_impl_docs/docs/extensions/ai.md)
- [Design Document](../../_impl_docs/design/027-ai-extension.md)

