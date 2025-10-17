// Package jobmanager provides functionality for running and managing Linux
// processes as Jobs.
//
// A Job represents a process that can be started, stopped, and monitored.
// Process output of a Job can be streamed concurrently to multiple clients.
//
// A Manager creates and manages Jobs, identified by UUID.
package jobmanager
