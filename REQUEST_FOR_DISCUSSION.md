# Request for Discussion - Job Worker Service

Design for a prototype job worker service that provides an API to run arbitrary Linux processes ([Challenge 1: Summary](https://github.com/gravitational/careers/blob/main/challenges/systems/challenge-1.md#summary)).

The solution consists of three main components:
 - [Library (`jobmanager`)](#Library) - The core functionality for managing jobs lifecycle.
 - [Server (`jobserver`)](#Server) - A gRPC server that exposes the functionality of the Library.
 - [CLI (`jobctl`)](#CLI) - A gRPC client to send requests to the Server.

### Constraints

 - 64-bit Linux kernel with support for cgroups v2.
 - System must have enough memory to store combined stdout/stderr of all jobs.
 - Server must be run as `root` user.

### Out of scope

 - Persistence of job state or output between server restarts, e.g. writing log output to disk or resuming job execution.
 - Intermediate operations or deletion of jobs, e.g. pause, restart, delete.
 - Process isolation or advanced Linux security features, e.g. namespaces, `pivot_root`, Seccomp, AppArmor.
 - Additional cgroup configuration, e.g. PID limits or CPU prioritisation.
 - Observability, e.g. tracing, metrics, structured logging (basic logging will be implemented).
 - High availability or scalability (service will run on a single machine).

## High-level design

### CLI

The `jobctl` CLI provides a user the ability to interact with the `jobserver` gRPC server via a gRPC client. Each CLI command will map to a server endpoint.

#### Configuration

All CLI commands will support the following configuration arguments, with hardcoded defaults for convenience.

| Argument | Type | Description | Default |
|-|-|-|-|
| `server-hostname` | string | Hostname of the server | `localhost` |
| `server-port` | int | Port on server | `8080` |
| `client-cert` | string | Path to client certificate | `certs/client.crt` |
| `client-key` | string | Path to client private key | `certs/client.key` |
| `ca-cert` | string | Path to CA certificate | `certs/ca.crt` |

##### Example

```bash
# Default configuration
$ jobctl start tail -f server.log

# Custom configuration
$ jobctl start \
	--server-hostname=localhost \
	--server-port=8080 \
	--client-cert=certs/client.crt \
	--client-key=certs/client.key \
	--ca-cert=certs/ca.crt \
	tail -f server.log
```

#### Start a job

`jobctl start` starts the specified job (with arguments, if provided) and returns its UUID.

```bash
# Just a command
$ jobctl start ls
fd7bdba1-3bfe-4af9-8e76-22fc4f22de26
```

```bash
# Command with arguments
$ jobctl start tail -f server.log
fd7bdba1-3bfe-4af9-8e76-22fc4f22de26
```

#### Get job status

`jobctl status <UUID>` gets the state and exit code (if available) of the job specified by the UUID.

```bash
# Job that hasn't exited
$ jobctl status fd7bdba1-3bfe-4af9-8e76-22fc4f22de26
STARTED

# Job that completed successfully
$ jobctl status fd7bdba1-3bfe-4af9-8e76-22fc4f22de26
STOPPED 0

# Job that completed with non-zero exit code
$ jobctl status fd7bdba1-3bfe-4af9-8e76-22fc4f22de26
STOPPED 1
```

#### Stream job output

`jobctl stream <UUID>` streams the combined stdout/stderr of the job specified by the UUID until the job exits.

```bash
$ jobctl stream fd7bdba1-3bfe-4af9-8e76-22fc4f22de26
<streamed-combined-stdout-stderr>
```

#### Stop a job

`jobctl stop <UUID>` stops the job specified by the UUID.

```bash
$ jobctl stop fd7bdba1-3bfe-4af9-8e76-22fc4f22de26
```

The `jobctl` CLI client will [authenticate with the `jobserver` gRPC server using mTLS](#Authentication).

### Server

The `jobserver` server is a gRPC server that receives requests from the `jobctl` CLI client, performs [authorisation](#Authorisation), and delegates handling of the requests to the [`jobmanager` library](#Library).

#### API

```proto
service JobService {
  rpc RunJob(RunJobRequest) returns (RunJobResponse);
  rpc StopJob(StopJobRequest) returns (StopJobResponse);
  rpc QueryJob(QueryJobRequest) returns (QueryJobResponse);
  rpc StreamJobOutput(StreamJobOutputRequest) returns (stream StreamJobOutputResponse);
}

message RunJobRequest {
  string program = 1; // Program to run as a job. 
  repeated string args = 2; // Arguments to pass to the program.
}

message RunJobResponse { 
  string id = 1; // ID of the started job.
}

message StopJobRequest {
  string id = 1; // ID of the job to stop.
}

message StopJobResponse {}

message QueryJobRequest {
  string id = 1; // ID of the job to query.
}

message QueryJobResponse { 
  JobState state = 1; // State of the job.
  int32 exit_code = 2; // Exit code of the job.
}

message StreamJobOutputRequest {
  string id = 1; // ID of the job to stream output from.
}

message StreamJobOutputResponse {
  bytes output = 1; // Streamed bytes from the jobs output.
}

enum JobState {
  JOB_STATE_UNSPECIFIED = 0;
  JOB_STATE_CREATED = 1; // Job configured and resources allocated successfully.
  JOB_STATE_STARTED = 2; // Program specified by job has started.
  JOB_STATE_STOPPING = 3; // Program specified by job is stopping, e.g. received SIGTERM but not yet exited.
  JOB_STATE_STOPPED = 4; // Program specified by job has exited with an exit code.
  JOB_STATE_FAILED = 5; // A failure as a result of an error returned from the service, e.g. server unable to allocate resources.
}
```

Errors will be handled using `google.golang.org/grpc/status` and `google.golang.org/grpc/codes`. For example:

```go
status.Error(codes.Internal, "Failed to run job")
```

#### Configuration

The `jobserver` CLI command will support the following configuration arguments, with hardcoded defaults for convenience.

| Argument | Type | Description | Default |
|-|-|-|-|
| `port` | int | gRPC server port | `8080` |
| `server-cert` | string | Path to server certificate | `certs/server.crt` |
| `server-key` | string | Path to server private key | `certs/server.key` |
| `ca-cert` | string | Path to CA certificate | `certs/ca.crt` |
| `debug` | boolean | Enable debug logs | `false` |
| `cpu-max-percent` | int | Per job CPU limit percentage (0-100, 0=unlimited) | `100` |
| `memory-max-mb` | int | Per job memory limit in MB (0=unlimited) | `512` |
| `io-max-mbps` | int | Per job I/O limit in MB/sec (0=unlimited) | `0` |

##### Example

```bash
# Default configuration
$ jobserver serve

# Custom configuration
$ jobserver serve \
	--port=8080 \
	--server-cert=certs/server.crt \
	--server-key=certs/server.key \
	--ca-cert=certs/ca.crt \
	--debug=false \
	--cpu-max-percent=100 \
	--memory-max-mb=512 \
	--io-max-mbps=0
```

### Library

The `jobmanager` library will provide the core functionality for managing the [lifecycle of Linux processes](#Process-execution-lifecycle) as 'jobs'.

#### `Job`

`Job` is a wrapper around an underlying process to be executed. The `Job` API will look something like: 

```go
// Job is a concurrency-safe abstraction around a process executed using exec.Cmd.
type Job struct {
  cmd *exec.Cmd
  mu sync.RWMutex
}

// NewJob creates a new job with the given id that will execute the program with the provided args.
func NewJob(id, program string, args []string) (*Job, error)

// ID gets the ID of the Job.
func (j *Job) ID() string
// Start starts the Job. Trying to start a job that is not in CREATED state returns an error.
func (j *Job) Start() error
// Stop stops the Job. Trying to stop a job that is not in STARTED state returns an error.
func (j *Job) Stop() error
// State returns the state of the job
func (j *Job) State() string
// ExitCode returns the exit code of the process. In the case no exit code is available, -1 will be returned.
func (j *Job) ExitCode() int
// Output returns a io.Reader for the process output (combined stdout/stderr).
func (j *Job) Output() io.Reader
// Done returns a recv-only channel that is used to signal completion of the job.
func (j *Job) Done() <-chan struct{}
```

A `Job` can be in one of the following states:

 - `CREATED` - Job configured and resources allocated successfully.
 - `STARTED` - Program specified by job has started.
 - `STOPPING` - Program specified by job is stopping, e.g. received SIGTERM but not yet exited.
 - `STOPPED` - Program specified by job has exited with an exit code.
 - `FAILED` - A failure as a result of an error returned from the service, e.g. server unable to allocate resources.

#### `JobManager`

The `JobManager` will be responsible for coordinating `Job` and `OutputManager`. The API for `JobManager` will look something like: 

```go
// JobManager is responsible for creating and tracking jobs, managing the lifecycle of jobs, and coordinating the output of jobs for streaming.
// It ensures safe concurrent access to a collection of jobs.
type JobManager struct {
  jobs map[string]*Job
  mu sync.RWMutex
}

// RunJob creates a UUID and creates a new Job with the provided program, args, and created UUID.
// It then adds the job to the collection in the JobManager and starts the job.
func (jm *JobManager) RunJob(program string, args []string) (*Job, error)
// StopJob calls the Stop method on the job with the given id.
func (jm *JobManager) StopJob(id string) error
// QueryJob calls the State and ExitCode methods on the job with the given id and combines their output into a Status.
func (jm *JobManager) QueryJob(id string) (Status, error)
// GetJob gets a reference to the job with the given id.
func (jm *JobManager) GetJob(id string) (*Job, error)
// StreamJobOutput returns an io.Reader with output from the job with the given id.
func (jm *JobManager) StreamJobOutput(id string) (io.Reader, error)
```

#### `OutputManager`

The `OutputManager` will be responsible for reading from the stdout/stderr pipe of a `Job`, maintaining a single buffer of output, and notifying subscribed clients of available output to read. The API for `OutputManager` will look something like:

```go
// OutputManager allows clients to subscribe to output from a job that's stored in an internal shared buffer.
type OutputManager struct {
  mu sync.Mutex
  cond *sync.Cond
  buffer []byte
  subscribers map[string]*subscriber
}

// Subscribe subscribes a client to a job's output.
func (om *OutputManager) Subscribe(id string) io.Reader
// Unsubscribe unsubscribes a client from a job's output.
func (om *OutputManager) Unsubscribe(id string) 
// Read reads a job's output into the internal shared buffer.
func (om *OutputManager) Read(p []byte) (n int, err error)
// Done returns a recv-only channel that signals when done.
func (om *OutputManager) Done() <-chan struct{}
// Stop terminates further subscriptions and reads, waiting for remaining clients to finish reading.
func (om *OutputManager) Stop()

// subscriber is used to track a clients read position in the buffer.
type subscriber struct {
  id string
  position int
}
```

## Security

### Authentication

Clients will connect to and authenticate with the server via mTLS (TLS 1.3). The following cipher suites are used and are not configurable when using TLS 1.3 with Go.
 - `TLS_AES_128_GCM_SHA256`
 - `TLS_AES_256_GCM_SHA384`
 - `TLS_CHACHA20_POLY1305_SHA256`

Certificates will use RSA encryption with a key size of 4096 bits. For convenience, certificates will be generated using `openssl`, with pregenerated CA, client and server certificates included in the repo.

To keep things simple, both the client and server will use the same certificate authority. To mitigate the risk of MITM attacks or clients being able to 'impersonate' trusted servers, the `KeyUsage` and `ExtendedKeyUsage` X.509 extensions will be used to ensure they're only used for their intended purpose.

### Authorisation

A simple role-based authorisation scheme will be implemented and will use the client certificate CN (Common Name) to identify the client.

Each operation supported by the service will have an associated permission.

| Operation | Permission |
|-|-|
| Start job | `job:start` |
| Stop job  | `job:stop` |
| Query job | `job:query` |
| Stream job output | `job:stream` |

Two roles be be supported.

| Role | Permissions |
|-|-|
| **Operator** | `job:start`, `job:stop`, `job:query`, `job:stream` |
| **Viewer** | `job:query`, `job:stream` |


Clients will be assigned a role, which will be hardcoded, e.g.

```go
clients := map[string]Role{
	"alice": RoleOperator,
	"bob":   RoleViewer,
}
```

Authorisation will be enforced using gRPC middleware interceptors that run before the request handlers, which will: 

1. Extract the common name to use as client identity (subject <- client certificate <- auth info <- peer <- request ctx).
1. Lookup the requested gRPC endpoint and role mapping.
1. Check if the client identity has a valid role for the requested gRPC endpoint.
     - If client is authorised, process the request.
     - If client is not authorised, return a `PermissionDenied` error.

## Process resource limits

Cgroups v2 will be used to impose CPU, memory, and disk I/O resource limits on jobs.

The cgroup will be created before the process starts, using the naming pattern: `jobworker-{UUID}`, and limits configured before the process is placed in it. The cgroup will be automatically cleaned up after the process exits.

The cgroup file descriptor will be passed to the process in `exec.Cmd` via the `SysProcAttr.CgroupFD` attribute to ensure the process is atomically placed in the cgroup when the process is exec'd, which will guarantee no opportunity for the process to violate the configured limits and prevent any child processes from escaping.

##### CPU

`cpu.max` will be used to limit CPU time. The period will be fixed at 100,000 and the quota will be calculated from the requested percentage limit by `(percent * period) / 100`.

The entry will be added like: `{quota} {period}` (in microseconds), for example: `50000 100000` (50%).

##### Memory

`memory.max` will be used to set a hard limit on memory usage.

The entry will be added like: `{limit_in_bytes}`, for example: `536870912` (512 MB).

##### Disk I/O

`io.max` will be used to limit disk I/O for the 'root' device. The device will be determined by reading `/proc/self/mountinfo` and finding the device mounted at `/`. To keep things simple, the same value will be used for reads and writes.

The entry will be added like: `{major}:{minor} rbps={limit_in_bytes} wbps={limit_in_bytes}`, for example `8:0 rbps=10485760 wbps=10485760` (10 MB/s).

### Testing

The following cgroups scenarios will be tested.

- Cgroups are created and removed successfully.
- Requested CPU, memory, I/O limits are written correctly.
- Processes placed in a cgroup respect the configured limits.
- Processes are placed in cgroups atomically.
- Child processes inherit cgroup membership.
- Edge cases: duplicates, non-existent, invalid limits.

## Output streaming

Output streaming will use a fan-out pattern to enable multiple clients to read output from the same job.

#### Setting up
1. `JobManager` creates a new `Job`.
  1. When a new `Job` is created, it will create a new `io.Pipe()`.
  1. The `io.PipeWriter` end of the pipe will be assigned to both the `Stdout` and `Stderr` of the `exec.Cmd`, combining them into a single stream.
  1. The `io.Reader` end of the pipe will be assigned to a field of the `Job` and returned when calling the `Output()` method.
1. `JobManager` creates a new `OutputManager`.
  1. When a new `OutputManager` is created, it will be passed the `io.Reader` end of the pipe from the `Job`. The `OutputManager` will start a background goroutine, in which it will:
     1. Read data from the pipe in chunks. 
     1. Append the data to a shared `[]byte` buffer.
     1. Call `cond.Broadcast()` on a `sync.Cond` condition variable to notify any client subscribers.

#### Client(s) streaming
1. When a client requests to stream output, the `JobManager` will call `Subscribe` on the `OutputManager` with the job ID.
1. The `OutputManager` will create a new 'subscription' with:
   - A unique subscriber ID.
   - A read position (initialised to `0`).
1. It will return a custom `io.Reader` implementation (a wrapper around the 'subscription') which, when read from will:
   1. Acquire a lock on the `OutputManager`.
   1. Read available data from the shared buffer at that subscriber's current position.
   1. Increment the read position by the number of bytes read.
   1. Release the lock.
   1. Return the bytes read.
1. When the read position reaches the current length of the buffer (i.e. is 'up-to-date'), the reader will call `cond.Wait()` to block on the condition variable until new data is available or the job completes.
1. When the `cond.Broadcast()` is called on new output available, the read continues.
1. When the background goroutine appends new data to the shared buffer, it calls `cond.Broadcast()`, notifying any waiting subscribers so they can read the new data.

#### Cleaning up
1. When the `Job` process exits, the `io.PipeWriter` end of the pipe will close.
1. The `OutputManager` will detect an `io.EOF` and exit its read loop.
1. Once subscribers have read all available data, they will read the `io.EOF` and exit.

Each client will be reading from the same underlying buffer in its own goroutine. This approach will enable performant fan-out (zero copies of the buffer) without slow clients blocking others, since they will only hold the lock long enough to read the bytes from the buffer.

The solution depends on unbounded growth of the underlying `[]byte` buffer. For the purposes of this challenge, that's acceptable. In a production system, we could look at segmenting off and writing to disk the historical data and maintaining only the most recent bytes in memory.

### Testing

The following output streaming scenarios will be tested.

- Single subscriber can read data.
- Multiple subscribers can read data.
- Data read by multiple subscribers matches.
- Late subscribers pick up historical data.
- Unsubscribe cleans up resources.
- Cancellation stops gracefully and cleans up.
- Edge cases: empty output, large output, rapid writes.

## Process execution lifecycle

### Creating a job

1. The `NewJob` function is called, which will create a new job and configure an `exec.Cmd` to execute the specified program and arguments.
1. The `exec.Cmd` will have the following `SysProcAttr.Setpgid` attribute set to `true` to place the process in its own process group, allowing signals to be sent to the parent and all children using a negative PID.
1. An `io.Pipe()` will be created, with the `io.Writer` end being assigned to both the stdout and stderr of the `exec.Cmd` to combine them. The `io.Reader` end will be stored for output streaming.
1. The job is set to `CREATED` state.

### Starting a job

1. The `job.Start()` method is called.
1. The current state is checked to be `CREATED`. If not, an error is returned.
1. The job is set to `STARTING` state.
1. A cgroup named `jobworker-{UUID}` is created at `/sys/fs/cgroup/`.
1. Resource limits will be written to `cpu.max`, `memory.max`, `io.max`.
1. The file descriptor for the cgroup is added to the `exec.Cmd` on the `SysProcAttr.CgroupFD` attribute and the `SysProcAttr.UseCgroupFD` attribute is set to `true`.
1. The `exec.Cmd` process is executed.
    - It's placed in its own process group.
    - It's placed in the cgroup.
1. A background goroutine will be spawned to call `process.Wait()` and monitor for process exit.
1. The job is set to `STARTED` state.

### Running job

1. The background goroutine blocks on the `process.Wait()` until the process exits.
1. While the process is running, it's writing stdout/stderr into the pipe. The `OutputManager` reads from the pipe and buffers data for clients.
1. The kernel enforces cgroup limits throughout the lifetime of the process.
1. Any child processes inherit the cgroup membership and process group.

### Stopping a job

1. The `job.Stop()` method is called.
1. The current state is checked to be `STARTED`. If not, an error is returned.
1. The job is set to `STOPPING` state.
1. A `SIGTERM` signal will be sent to the negative PID to initiate graceful shutdown of the process group with a context timeout.
    - If the timeout expires, a `SIGKILL` signal will be sent to force termination.
1. The job is set to `STOPPED` state.

### Cleaning up

1. Process exits.
1. The cgroup is removed.
1. The `io.Writer` end of the pipe is closed, signaling `io.EOF` to the `OutputManager`.
1. The jobs `Done()` channel is closed, signaling completion.
1. The `OutputManager` (on receiving `io.EOF`), stops reading and broadcasts to all subscribers, then its `Done()` channel is closed.

### Testing

The following process execution lifecycle scenarios will be tested.

- Basic operations work correctly: start, stop, query, stream.
- Invalid state transitions are prevented.
- Exit codes and states are handled correctly.
- Signals are correctly handled, e.g. SIGTERM then timeout to SIGKILL.
- Process stdout/stderr combined and written; close on process exit.
- Process is started in cgroup with limits applied correctly.
- Safe concurrent access to Job methods, e.g. calling the same method multiple times.
- Edge cases: non-existent programs, crashes, permission errors.

## Trade-offs

## Notes
- CLIs will use `github.com/spf13/cobra-cli`.
- UUIDs will be generated using `github.com/google/uuid`.

