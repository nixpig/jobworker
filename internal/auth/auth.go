package auth

import (
	"context"
	"fmt"
	"slices"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

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

var RolePermissions = map[Role][]Permission{
	RoleOperator: {
		PermissionJobStart,
		PermissionJobStop,
		PermissionJobQuery,
		PermissionJobStream,
	},
	RoleViewer: {PermissionJobQuery, PermissionJobStream},
}

var EndpointPermissions = map[string]Permission{
	"/job.v1.JobService/RunJob":          PermissionJobStart,
	"/job.v1.JobService/StopJob":         PermissionJobStop,
	"/job.v1.JobService/QueryJob":        PermissionJobQuery,
	"/job.v1.JobService/StreamJobOutput": PermissionJobStream,
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

func IsAuthorised(clientRole Role, endpoint string) error {
	requiredPermissions, exists := EndpointPermissions[endpoint]
	if !exists {
		return fmt.Errorf("specified endpoint not in endpoint permissions")
	}

	permissions, ok := RolePermissions[clientRole]
	if !ok {
		return fmt.Errorf("specified role not in role permissions")
	}

	if !slices.Contains(permissions, requiredPermissions) {
		return fmt.Errorf("required permission not in permissions for role")
	}

	return nil
}

func Authorise(ctx context.Context, method string) error {
	_, ou, err := GetClientIdentity(ctx)
	if err != nil {
		return fmt.Errorf("get client identity: %w", err)
	}

	clientRole := Role(ou)

	if err := IsAuthorised(clientRole, method); err != nil {
		return fmt.Errorf("authorise client: %w", err)
	}

	return nil
}
