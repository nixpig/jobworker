package auth

import (
	"context"
	"slices"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
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
		return "", "", status.Error(codes.Unauthenticated, "not authenticated")
	}

	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return "", "", status.Error(codes.Unauthenticated, "not authenticated")
	}

	if len(tlsInfo.State.VerifiedChains) == 0 ||
		len(tlsInfo.State.VerifiedChains[0]) == 0 {
		return "", "", status.Error(codes.Unauthenticated, "not authenticated")
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
	requiredPermission, exists := endpointPermissions[endpoint]
	if !exists {
		return status.Error(codes.Unimplemented, "unknown endpoint")
	}

	perms, ok := rolePermissions[role]
	if !ok {
		return status.Error(codes.PermissionDenied, "unknown role")
	}

	if slices.Contains(perms, requiredPermission) {
		return status.Error(codes.PermissionDenied, "not authorised")
	}

	return nil
}
