# GraphQL Basic Example

A simple example demonstrating the GraphQL extension for Forge v2.

## Running

```bash
cd v2/examples/graphql-basic
go run main.go
```

## Endpoints

- GraphQL API: http://localhost:8080/graphql
- Playground: http://localhost:8080/playground

## Try It Out

### Using cURL

```bash
# Hello query
curl -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '{"query":"{ hello(name: \"World\") }"}'

# Version query
curl -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '{"query":"{ version }"}'

# Echo mutation
curl -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '{"query":"mutation { echo(message: \"Hello GraphQL\") }"}'
```

### Using Playground

Visit http://localhost:8080/playground and try:

```graphql
query {
  hello(name: "Forge")
  version
}

mutation {
  echo(message: "Testing mutations")
}
```

## Features Demonstrated

- ✅ Query operations
- ✅ Mutation operations
- ✅ GraphQL Playground
- ✅ Automatic schema generation
- ✅ Type-safe resolvers
- ✅ Metrics and logging
- ✅ Query caching

