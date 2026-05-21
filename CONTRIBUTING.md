# Contributing to the InstaNode Go SDK

## Filing issues

Bugs and feature requests welcome at https://github.com/InstaNode-dev/sdk-go/issues.

## Workflow

```
git clone https://github.com/InstaNode-dev/sdk-go
cd sdk-go
go build ./...
go vet ./...
go test ./... -short -p 1
```

All three must pass before opening a PR.

## Quickstart for examples

```
go run ./examples/agent-bootstrap
go run ./examples/provision-all
```

Set `INSTANODE_API_URL=http://localhost:8080` to point at a local api instance.

## Style

- Follow existing patterns. The package is intentionally small + dependency-light.
- New public symbols get godoc comments.
- Tests next to source.
- Errors should wrap the underlying transport error so callers can `errors.As` to typed error values.

## PR checklist

- `go build ./...` green
- `go vet ./...` green
- `go test ./... -short -p 1` green
- New public symbol → godoc
- New behaviour → test
- API-contract changes mirrored against the api OpenAPI spec at https://api.instanode.dev/openapi.json

## License

MIT. By contributing, you agree your contributions are licensed under the same.
