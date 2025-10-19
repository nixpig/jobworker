package main

import (
	"context"
	"fmt"
	"log/slog"
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
	"/job.v1.JobService/RunJob":          PermissionJobStart,
	"/job.v1.JobService/StopJob":         PermissionJobStop,
	"/job.v1.JobService/QueryJob":        PermissionJobQuery,
	"/job.v1.JobService/StreamJobOutput": PermissionJobStream,
}

func getClientIdentity(ctx context.Context) (string, string, error) {
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

func isAuthorised(role Role, endpoint string) error {
	requiredPermissions, exists := endpointPermissions[endpoint]
	if !exists {
		return fmt.Errorf("specified endpoint not in endpoint permissions")
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

func authorise(ctx context.Context, method string, logger *slog.Logger) error {
	cn, ou, err := getClientIdentity(ctx)
	if err != nil {
		logger.Warn("failed to get client identity", "err", err)
		return status.Error(codes.Unauthenticated, "not authenticated")
	}

	role := Role(ou)

	if err := isAuthorised(role, method); err != nil {
		logger.Warn(
			"failed to authorise client",
			"cn", cn,
			"ou", ou,
			"role", role,
			"method", method,
			"err", err,
		)

		return status.Error(codes.PermissionDenied, "not authorised")
	}

	logger.Debug(
		"authorised client request",
		"cn", cn,
		"ou", ou,
		"role", role,
		"method", method,
	)

	return nil
}
