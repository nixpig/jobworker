# jobworker

[WIP] Prototype jobworker service that provides an API to run arbitrary Linux processes.

## Generate certs

```bash
make certs
```

## `jobserver`

```bash
make build-server

./tmp/bin/jobserver
```

## `jobctl`

```bash
make build-cli

./tmp/bin/jobctl help
```

## Development

### Run tests

```bash
make test
```
