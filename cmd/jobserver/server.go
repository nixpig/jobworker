package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	api "github.com/nixpig/jobworker/api/v1"
	"github.com/nixpig/jobworker/internal/auth"
	"github.com/nixpig/jobworker/internal/jobmanager"
	"github.com/nixpig/jobworker/internal/jobmanager/cgroups"
	"github.com/nixpig/jobworker/internal/tlsconfig"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

const (
	// streamBufferSize is the buffer size for reading job output.
	// 4KB aligns with typical pipe buffer sizes.
	streamBufferSize = 4096
)

type server struct {
	api.UnimplementedJobServiceServer

	manager    *jobmanager.Manager
	logger     *slog.Logger
	cfg        *config
	grpcServer *grpc.Server
	addr       string
}

func newServer(
	manager *jobmanager.Manager,
	logger *slog.Logger,
	cfg *config,
) *server {
	return &server{manager: manager, logger: logger, cfg: cfg}
}

func (s *server) start(listener net.Listener) error {
	tlsConfig, err := tlsconfig.SetupTLS(&tlsconfig.Config{
		CertPath:   s.cfg.certPath,
		KeyPath:    s.cfg.keyPath,
		CACertPath: s.cfg.caCertPath,
		Server:     true,
	})
	if err != nil {
		return fmt.Errorf("setup TLS config: %w", err)
	}

	s.grpcServer = grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			contextCheckUnaryInterceptor(s.logger),
			authUnaryInterceptor(s.logger),
		),
		grpc.ChainStreamInterceptor(
			authStreamInterceptor(s.logger),
		),
		grpc.Creds(credentials.NewTLS(tlsConfig)),
	)

	api.RegisterJobServiceServer(s.grpcServer, s)

	s.addr = listener.Addr().String()

	return s.grpcServer.Serve(listener)
}

func (s *server) shutdown() {
	if s.grpcServer == nil {
		s.logger.Warn("no gRPC server started")
		return
	}

	doneCh := make(chan struct{}, 1)
	go func() {
		s.grpcServer.GracefulStop()
		close(doneCh)
	}()

	select {
	case <-doneCh:
	case <-time.After(10 * time.Second):
		s.logger.Warn("graceful shutdown timed out, forcing stop")
		s.grpcServer.Stop()
	}
}

// TODO: Add healthcheck methods for readiness/liveness probes.

func (s *server) RunJob(
	ctx context.Context,
	req *api.RunJobRequest,
) (*api.RunJobResponse, error) {
	if req.Program == "" {
		return nil, status.Error(codes.InvalidArgument, "Program is empty")
	}

	id, err := s.manager.RunJob(
		req.Program,
		req.Args,
		&cgroups.ResourceLimits{
			CPUMaxPercent:  50,
			MemoryMaxBytes: 536870912,
			IOMaxBPS:       10485760,
		},
	)
	if err != nil {
		return nil, s.mapError("run job", err)
	}

	return &api.RunJobResponse{Id: id}, nil
}

func (s *server) StopJob(
	ctx context.Context,
	req *api.StopJobRequest,
) (*api.StopJobResponse, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "ID is empty")
	}

	if err := s.manager.StopJob(req.Id); err != nil {
		return nil, s.mapError("stop job", err)
	}

	return &api.StopJobResponse{}, nil
}

func (s *server) QueryJob(
	ctx context.Context,
	req *api.QueryJobRequest,
) (*api.QueryJobResponse, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "ID is empty")
	}

	jobStatus, err := s.manager.QueryJob(req.Id)
	if err != nil {
		return nil, s.mapError("query job", err)
	}

	signal := ""
	if jobStatus.Signal != nil {
		signal = jobStatus.Signal.String()
	}

	return &api.QueryJobResponse{
		State:       api.JobState(jobStatus.State),
		ExitCode:    int32(jobStatus.ExitCode),
		Signal:      signal,
		Interrupted: jobStatus.Interrupted,
	}, nil
}

func (s *server) StreamJobOutput(
	req *api.StreamJobOutputRequest,
	stream api.JobService_StreamJobOutputServer,
) error {
	if req.Id == "" {
		return status.Error(codes.InvalidArgument, "ID is empty")
	}

	// TODO: If we end up with more than one streaming method then create an
	// interceptor for the context check, like has been done for unary methods.
	// Not worth the hassle for a single method though.
	if stream.Context().Err() != nil {
		return status.FromContextError(stream.Context().Err()).Err()
	}

	outputReader, err := s.manager.StreamJobOutput(req.Id)
	if err != nil {
		return s.mapError("output stream", err)
	}

	var closeOnce sync.Once
	closeReader := func() {
		if err := outputReader.Close(); err != nil {
			s.logger.Debug("close output reader", "id", req.Id, "err", err)
		}
	}
	defer closeOnce.Do(closeReader)

	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	go func() {
		<-ctx.Done()
		closeOnce.Do(closeReader)
	}()

	buf := make([]byte, streamBufferSize)
	for {
		if stream.Context().Err() != nil {
			s.logger.Debug("stream cancelled by client", "id", req.Id)
			return status.FromContextError(stream.Context().Err()).Err()
		}

		n, err := outputReader.Read(buf)
		if n > 0 {
			if err := stream.Send(&api.StreamJobOutputResponse{
				Output: buf[:n],
			}); err != nil {
				s.logger.Warn("stream data to client", "id", req.Id, "err", err)
				return status.Error(codes.DataLoss, "failed to stream data")
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}

			return s.mapError("read job output stream", err)
		}
	}

	return nil
}

// mapError translates jobmanager errors to gRPC errors.
func (s *server) mapError(logMsg string, err error) error {
	switch {
	case errors.Is(err, jobmanager.ErrJobNotFound):
		s.logger.Warn(logMsg, "err", err)
		return status.Error(codes.NotFound, err.Error())

	case errors.As(err, new(jobmanager.InvalidStateError)):
		s.logger.Warn(logMsg, "err", err)
		return status.Error(codes.FailedPrecondition, err.Error())

	default:
		s.logger.Error(logMsg, "err", err)
		return status.Error(codes.Internal, "internal server error")
	}
}

// contextCheckUnaryInterceptor rejects requests with a cancelled context.
func contextCheckUnaryInterceptor(
	logger *slog.Logger,
) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if ctx.Err() != nil {
			logger.Debug("request context cancelled", "err", ctx.Err())

			return nil, status.FromContextError(ctx.Err()).Err()
		}

		return handler(ctx, req)
	}
}

// authUnaryInterceptor authorises clients for unary methods.
func authUnaryInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if err := auth.Authorise(ctx, info.FullMethod); err != nil {
			logger.Warn("failed to authorise client", "err", err)
			return nil, status.Error(codes.PermissionDenied, "not authorised")
		}

		return handler(ctx, req)
	}
}

// authStreamInterceptor authorises clients for streaming methods.
func authStreamInterceptor(logger *slog.Logger) grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		if err := auth.Authorise(ss.Context(), info.FullMethod); err != nil {
			logger.Warn("failed to authorise client", "err", err)
			return status.Error(codes.PermissionDenied, "not authorised")
		}

		return handler(srv, ss)
	}
}
