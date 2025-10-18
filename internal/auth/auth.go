// Package auth provides the auth model and utilities for working with auth
// for the job service.
package auth

import (
	"context"
	"fmt"
	"slices"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

// Permission represents an action that can be performed on the job service.
type Permission string

const (
	PermissionJobStart  Permission = "job:start"
	PermissionJobStop   Permission = "job:stop"
	PermissionJobQuery  Permission = "job:query"
	PermissionJobStream Permission = "job:stream"
)

// Role is used to determine what collection of Permissions a client has.
type Role string

const (
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

// rolePermissions defines what Permissions each Role has.
var rolePermissions = map[Role][]Permission{
	RoleOperator: {
		PermissionJobStart,
		PermissionJobStop,
		PermissionJobQuery,
		PermissionJobStream,
	},
	RoleViewer: {PermissionJobQuery, PermissionJobStream},
}

// methodPermissions maps gRPC methods to the required Permission.
var methodPermissions = map[string]Permission{
	"/job.v1.JobService/RunJob":          PermissionJobStart,
	"/job.v1.JobService/StopJob":         PermissionJobStop,
	"/job.v1.JobService/QueryJob":        PermissionJobQuery,
	"/job.v1.JobService/StreamJobOutput": PermissionJobStream,
}

// TODO: Add unit tests for production solution. Currently relying on
// integration tests which exercise all code paths, but take longer to run and
// don't cover all the potential edge cases.

// GetClientIdentity extracts and returns the Common Name (CN) and
// OrganizationalUnit (OU) fields from a client's mTLS certificate on the given
// gRPC context
func GetClientIdentity(ctx context.Context) (string, string, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return "", "", fmt.Errorf("failed to get peer info from context")
	}

	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return "", "", fmt.Errorf("failed to get TLS info from peer auth info")
	}

	if len(tlsInfo.State.VerifiedChains) == 0 ||
		len(tlsInfo.State.VerifiedChains[0]) == 0 {
		return "", "", fmt.Errorf("no verified chains in TLS info")
	}

	cert := tlsInfo.State.VerifiedChains[0][0]

	cn := cert.Subject.CommonName

	var ou string
	if len(cert.Subject.OrganizationalUnit) > 0 {
		ou = cert.Subject.OrganizationalUnit[0]
	}

	return cn, ou, nil
}

// IsAuthorised checks if the given Role has Permission to access the given
// gRPC method. Returns nil if authorised, or error if not.
func IsAuthorised(role Role, method string) error {
	requiredPermissions, exists := methodPermissions[method]
	if !exists {
		return fmt.Errorf("specified method not in method permissions")
	}

	permissions, ok := rolePermissions[role]
	if !ok {
		return fmt.Errorf("specified role not in role permissions")
	}

	if !slices.Contains(permissions, requiredPermissions) {
		return fmt.Errorf("required permission not in permissions for role")
	}

	return nil
}

// Authorise verifies that the client in the gRPC context has permissions to
// call the given method.
func Authorise(ctx context.Context, method string) error {
	_, ou, err := GetClientIdentity(ctx)
	if err != nil {
		return fmt.Errorf("get client identity: %w", err)
	}

	role := Role(ou)

	if _, exists := rolePermissions[role]; !exists {
		return fmt.Errorf("unknown role: %s", ou)
	}

	if err := IsAuthorised(role, method); err != nil {
		return fmt.Errorf("authorise client: %w", err)
	}

	return nil
}
