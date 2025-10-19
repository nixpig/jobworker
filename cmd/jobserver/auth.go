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

type permission string

const (
	permissionJobStart  permission = "job:start"
	permissionJobStop   permission = "job:stop"
	permissionJobQuery  permission = "job:query"
	permissionJobStream permission = "job:stream"
)

type role string

const (
	roleOperator role = "operator"
	roleViewer   role = "viewer"
)

var rolePermissions = map[role][]permission{
	roleOperator: {
		permissionJobStart,
		permissionJobStop,
		permissionJobQuery,
		permissionJobStream,
	},
	roleViewer: {permissionJobQuery, permissionJobStream},
}

var endpointPermissions = map[string]permission{
	"/job.v1.JobService/RunJob":          permissionJobStart,
	"/job.v1.JobService/StopJob":         permissionJobStop,
	"/job.v1.JobService/QueryJob":        permissionJobQuery,
	"/job.v1.JobService/StreamJobOutput": permissionJobStream,
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

func isAuthorised(clientRole role, endpoint string) error {
	requiredPermissions, exists := endpointPermissions[endpoint]
	if !exists {
		return fmt.Errorf("specified endpoint not in endpoint permissions")
	}

	permissions, ok := rolePermissions[clientRole]
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

	clientRole := role(ou)

	if err := isAuthorised(clientRole, method); err != nil {
		logger.Warn(
			"failed to authorise client",
			"cn", cn,
			"ou", ou,
			"role", clientRole,
			"method", method,
			"err", err,
		)

		return status.Error(codes.PermissionDenied, "not authorised")
	}

	logger.Debug(
		"authorised client request",
		"cn", cn,
		"ou", ou,
		"role", clientRole,
		"method", method,
	)

	return nil
}
