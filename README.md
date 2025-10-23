# jobworker

A prototype jobworker service that provides an API and CLI to run arbitrary Linux processes on a remote server.

## Server

1. Build the server binary: `make build-server`
2. Start the server: `sudo ./tmp/bin/jobserver --debug`

The server has a reasonable default configuration. To see available configuration options, just run `./tmp/bin/jobserver --help`.

Note: without additional system configuration (out-of-scope for this prototype), the `jobserver` needs to be run with `sudo` user to use cgroups.

## Client CLI

1. Build the CLI binary: `make build-cli`
2. See available commands: `./tmp/bin/jobctl --help`

The client CLI has a reasonable default configuration. To see additional configuration options, just run `./tmp/bin/jobctl --help`.

### Examples

- Start a job: `jobctl start tail -f server.log`
- Query status of a job: `jobctl status 9302033c-f8f7-4b6e-9363-a7aa201cce1b`
- Stream output of a job: `jobctl stream 9302033c-f8f7-4b6e-9363-a7aa201cce1b`
- Stop a job: `jobctl stop 9302033c-f8f7-4b6e-9363-a7aa201cce1b`

## Auth

The Client CLI authenticates with the Server using mTLS. Pre-generated certificates are included in the `certs/` directory. The default configuration of the Client CLI and Server will pick these up.

If you do need to regenerate any certs, just run `make certs`.

## Tests
 - Run only unit tests: `make test`
 - Run E2E and unit tests: `make test-e2e`

