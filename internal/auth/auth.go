package auth

import (
	"context"
	"fmt"
	"slices"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

// TODO: Add unit tests in a production system. Omitting for now as testing
// all the permutations will test too long and these will be generally well
// exercised by the server integration tests.

type Permission string

const (
	PermissionJobStart  Permission = "job:start"
	PermissionJobStop   Permission = "job:stop"
	PermissionJobQuery  Permission = "job:query"
	PermissionJobStream Permission = "job:stream"
)

type Role string

const (
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

var rolePermissions = map[Role][]Permission{
	RoleOperator: {
		PermissionJobStart,
		PermissionJobStop,
		PermissionJobQuery,
		PermissionJobStream,
	},
	RoleViewer: {PermissionJobQuery, PermissionJobStream},
}

var endpointPermissions = map[string]Permission{
	"jobworker.JobService/RunJob":          PermissionJobStart,
	"jobworker.JobService/StopJob":         PermissionJobStop,
	"jobworker.JobService/QueryJob":        PermissionJobQuery,
	"jobworker.JobService/StreamJobOutput": PermissionJobStream,
}

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

func IsAuthorised(role Role, endpoint string) error {
	requiredPermissions, exists := endpointPermissions[endpoint]
	if !exists {
		return fmt.Errorf("specified endpoint not in endpoint permissions")
	}

	permissions, ok := rolePermissions[role]
	if !ok {
		return fmt.Errorf("specified role not in role permissions")
	}

	if slices.Contains(permissions, requiredPermissions) {
		return fmt.Errorf("required permission not in permissions for role")
	}

	return nil
}
