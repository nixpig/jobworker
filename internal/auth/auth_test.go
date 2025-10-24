package auth_test

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"testing"

	api "github.com/nixpig/jobworker/api/v1"
	"github.com/nixpig/jobworker/internal/auth"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

func createAuthInfo(t *testing.T, cn, ou string) credentials.TLSInfo {
	t.Helper()

	cert := &x509.Certificate{
		Subject: pkix.Name{
			CommonName:         cn,
			OrganizationalUnit: []string{ou},
		},
	}

	return credentials.TLSInfo{
		State: tls.ConnectionState{
			VerifiedChains: [][]*x509.Certificate{{cert}},
		},
	}
}

func TestIsAuthorised(t *testing.T) {
	t.Parallel()

	scenarios := map[string]struct {
		role         auth.Role
		method       string
		isAuthorised bool
	}{
		"Test operator can run job": {
			role:         auth.RoleOperator,
			method:       api.JobService_RunJob_FullMethodName,
			isAuthorised: true,
		},
		"Test operator can stop job": {
			role:         auth.RoleOperator,
			method:       api.JobService_StopJob_FullMethodName,
			isAuthorised: true,
		},
		"Test operator can query job": {
			role:         auth.RoleOperator,
			method:       api.JobService_QueryJob_FullMethodName,
			isAuthorised: true,
		},
		"Test operator can stream job output": {
			role:         auth.RoleOperator,
			method:       api.JobService_StreamJobOutput_FullMethodName,
			isAuthorised: true,
		},

		"Test viewer cannot run job": {
			role:         auth.RoleViewer,
			method:       api.JobService_RunJob_FullMethodName,
			isAuthorised: false,
		},
		"Test viewer cannot stop job": {
			role:         auth.RoleViewer,
			method:       api.JobService_RunJob_FullMethodName,
			isAuthorised: false,
		},
		"Test viewer can query job": {
			role:         auth.RoleViewer,
			method:       api.JobService_QueryJob_FullMethodName,
			isAuthorised: true,
		},
		"Test viewer can stream job output": {
			role:         auth.RoleViewer,
			method:       api.JobService_StreamJobOutput_FullMethodName,
			isAuthorised: true,
		},

		"Test unknown method returns error": {
			role:         auth.RoleOperator,
			method:       "/job.v1.JobService/Unknown",
			isAuthorised: false,
		},
		"Test unknown role returns error": {
			role:         auth.Role("Unknown"),
			method:       api.JobService_StreamJobOutput_FullMethodName,
			isAuthorised: false,
		},
	}

	for scenario, config := range scenarios {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()

			err := auth.IsAuthorised(config.role, config.method)

			if config.isAuthorised && err != nil {
				t.Errorf(
					"expected is authorised check not to return error: got '%v'",
					err,
				)
			}

			if !config.isAuthorised && err == nil {
				t.Errorf("expected not authorised check to return error")
			}
		})
	}
}

func TestMethodsHavePermissions(t *testing.T) {
	t.Parallel()

	t.Run("Test all methods have permissions assigned", func(t *testing.T) {
		for _, m := range api.JobService_ServiceDesc.Methods {
			fullMethodName := fmt.Sprintf(
				"/%s/%s",
				api.JobService_ServiceDesc.ServiceName,
				m.MethodName,
			)

			if _, exists := auth.MethodPermissions[fullMethodName]; !exists {
				t.Errorf(
					"gRPC method missing permission assignment: '%v'",
					fullMethodName,
				)
			}
		}
	})
}

func TestGetClientIdentity(t *testing.T) {
	t.Parallel()

	t.Run("Test peer with valid TLS info", func(t *testing.T) {
		p := &peer.Peer{AuthInfo: createAuthInfo(t, "alice", "operator")}

		ctx := peer.NewContext(t.Context(), p)

		cn, ou, err := auth.GetClientIdentity(ctx)
		if err != nil {
			t.Errorf(
				"expected get client identity not to return error: got '%v'",
				err,
			)
		}

		wantCN := "alice"
		if cn != wantCN {
			t.Errorf("expected CN: got '%s', want '%s'", cn, wantCN)
		}

		wantOU := "operator"
		if ou != wantOU {
			t.Errorf("expected OU: got '%s', want '%s'", ou, wantOU)
		}
	})

	t.Run("Test peer with no TLS info", func(t *testing.T) {
		p := &peer.Peer{AuthInfo: nil}

		ctx := peer.NewContext(t.Context(), p)

		cn, ou, err := auth.GetClientIdentity(ctx)
		if err == nil {
			t.Errorf("expected get client identity to return error")
		}

		if cn != "" {
			t.Errorf("expected CN to be empty: got '%s'", cn)
		}

		if ou != "" {
			t.Errorf("expected OU to be empty: got '%s'", cn)
		}
	})

	t.Run("Test no peer in context", func(t *testing.T) {
		ctx := t.Context()

		cn, ou, err := auth.GetClientIdentity(ctx)
		if err == nil {
			t.Errorf("expected get client identity to return error")
		}

		if cn != "" {
			t.Errorf("expected CN to be empty: got '%s'", cn)
		}

		if ou != "" {
			t.Errorf("expected OU to be empty: got '%s'", ou)
		}
	})
}

func TestAuthorise(t *testing.T) {
	t.Parallel()

	t.Run("Test operator can run job", func(t *testing.T) {
		p := &peer.Peer{AuthInfo: createAuthInfo(t, "alice", "operator")}

		ctx := peer.NewContext(t.Context(), p)

		if err := auth.Authorise(ctx, api.JobService_RunJob_FullMethodName); err != nil {
			t.Errorf("expected authorise not to return error: got '%v'", err)
		}
	})

	t.Run("Test viewer cannot run job", func(t *testing.T) {
		p := &peer.Peer{AuthInfo: createAuthInfo(t, "bob", "viewer")}

		ctx := peer.NewContext(t.Context(), p)

		if err := auth.Authorise(ctx, api.JobService_RunJob_FullMethodName); err == nil {
			t.Errorf("expected authorise to return error")
		}

	})

	t.Run("Test viewer can query job", func(t *testing.T) {
		p := &peer.Peer{AuthInfo: createAuthInfo(t, "bob", "viewer")}

		ctx := peer.NewContext(t.Context(), p)

		if err := auth.Authorise(ctx, api.JobService_QueryJob_FullMethodName); err != nil {
			t.Errorf("expected query job not to return error: got '%v'", err)
		}
	})

	t.Run("Test unknown role", func(t *testing.T) {
		p := &peer.Peer{AuthInfo: createAuthInfo(t, "charlie", "admin")}

		ctx := peer.NewContext(t.Context(), p)

		if err := auth.Authorise(ctx, api.JobService_QueryJob_FullMethodName); err == nil {
			t.Errorf("expected query job to return error")
		}
	})

	t.Run("Test invalid context", func(t *testing.T) {
		ctx := t.Context()

		if err := auth.Authorise(ctx, api.JobService_QueryJob_FullMethodName); err == nil {
			t.Errorf("expected authorise to return error")
		}
	})
}
