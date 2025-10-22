//go:build !e2e

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

func TestIsAuthorised(t *testing.T) {
	t.Parallel()

	scenarios := map[string]struct {
		role         auth.Role
		method       string
		isAuthorised bool
	}{
		"Test operator can run job": {
			role:         auth.RoleOperator,
			method:       "/job.v1.JobService/RunJob",
			isAuthorised: true,
		},
		"Test operator can stop job": {
			role:         auth.RoleOperator,
			method:       "/job.v1.JobService/StopJob",
			isAuthorised: true,
		},
		"Test operator can query job": {
			role:         auth.RoleOperator,
			method:       "/job.v1.JobService/QueryJob",
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
					"expected authorised not to return error: got '%v'",
					err,
				)
			}

			if !config.isAuthorised && err == nil {
				t.Errorf("expected not authorised to return error")
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
					"gRPC method doesn't have permission assigned: '%v'",
					fullMethodName,
				)
			}
		}
	})
}

func TestGetClientIdentity(t *testing.T) {
	t.Parallel()

	t.Run("Test peer with valid TLS info", func(t *testing.T) {
		cert := &x509.Certificate{
			Subject: pkix.Name{
				CommonName:         "alice",
				OrganizationalUnit: []string{"operator"},
			},
		}

		authInfo := credentials.TLSInfo{
			State: tls.ConnectionState{
				VerifiedChains: [][]*x509.Certificate{{cert}},
			},
		}

		p := &peer.Peer{AuthInfo: authInfo}

		ctx := peer.NewContext(t.Context(), p)

		cn, ou, err := auth.GetClientIdentity(ctx)
		if err != nil {
			t.Errorf("expected not to receive error: got '%v'", err)
		}

		if cn != "alice" {
			t.Errorf("expected CN: got '%s', want 'alice'", cn)
		}

		if ou != "operator" {
			t.Errorf("expected OU: got '%s', want 'operator'", cn)
		}
	})

	t.Run("Test peer with no TLS info", func(t *testing.T) {
		p := &peer.Peer{AuthInfo: nil}

		ctx := peer.NewContext(t.Context(), p)

		cn, ou, err := auth.GetClientIdentity(ctx)
		if err == nil {
			t.Errorf("expected to receive error")
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
			t.Errorf("expected to receive error")
		}

		if cn != "" {
			t.Errorf("expected CN to be empty: got '%s'", cn)
		}

		if ou != "" {
			t.Errorf("expected OU to be empty: got '%s'", cn)
		}
	})

}

func TestAuthorise(t *testing.T) {
	t.Parallel()

	t.Run("Test operator can run job", func(t *testing.T) {
		cert := &x509.Certificate{
			Subject: pkix.Name{
				CommonName:         "alice",
				OrganizationalUnit: []string{"operator"},
			},
		}

		authInfo := credentials.TLSInfo{
			State: tls.ConnectionState{
				VerifiedChains: [][]*x509.Certificate{{cert}},
			},
		}

		p := &peer.Peer{AuthInfo: authInfo}

		ctx := peer.NewContext(t.Context(), p)

		if err := auth.Authorise(ctx, "/job.v1.JobService/RunJob"); err != nil {
			t.Errorf("expected not to receive error: got '%v'", err)
		}
	})

	t.Run("Test viewer cannot run job", func(t *testing.T) {
		cert := &x509.Certificate{
			Subject: pkix.Name{
				CommonName:         "bob",
				OrganizationalUnit: []string{"viewer"},
			},
		}

		authInfo := credentials.TLSInfo{
			State: tls.ConnectionState{
				VerifiedChains: [][]*x509.Certificate{{cert}},
			},
		}

		p := &peer.Peer{AuthInfo: authInfo}

		ctx := peer.NewContext(t.Context(), p)

		if err := auth.Authorise(ctx, "/job.v1.JobService/RunJob"); err == nil {
			t.Errorf("expected to receive error")
		}

	})

	t.Run("Test viewer can query job", func(t *testing.T) {
		cert := &x509.Certificate{
			Subject: pkix.Name{
				CommonName:         "bob",
				OrganizationalUnit: []string{"viewer"},
			},
		}

		authInfo := credentials.TLSInfo{
			State: tls.ConnectionState{
				VerifiedChains: [][]*x509.Certificate{{cert}},
			},
		}

		p := &peer.Peer{AuthInfo: authInfo}

		ctx := peer.NewContext(t.Context(), p)

		if err := auth.Authorise(ctx, "/job.v1.JobService/QueryJob"); err != nil {
			t.Errorf("expected not to receive error: got '%v'", err)
		}
	})

	t.Run("Test unknown role", func(t *testing.T) {
		cert := &x509.Certificate{
			Subject: pkix.Name{
				CommonName:         "charlie",
				OrganizationalUnit: []string{"admin"},
			},
		}

		authInfo := credentials.TLSInfo{
			State: tls.ConnectionState{
				VerifiedChains: [][]*x509.Certificate{{cert}},
			},
		}

		p := &peer.Peer{AuthInfo: authInfo}

		ctx := peer.NewContext(t.Context(), p)

		if err := auth.Authorise(ctx, "/job.v1.JobService/QueryJob"); err == nil {
			t.Errorf("expected to receive error")
		}
	})

	t.Run("Test invalid context", func(t *testing.T) {
		ctx := t.Context()

		if err := auth.Authorise(ctx, "/job.v1.JobService/QueryJob"); err == nil {
			t.Errorf("expected to receive error")
		}
	})
}
