package main

import (
	"bytes"
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

	// TODO: Make shutdownTimeout configurable via server config.
	shutdownTimeout = 10 * time.Second
)

var (
	errIDRequired = status.Error(
		codes.InvalidArgument,
		"ID is required",
	)
	errProgramRequired = status.Error(
		codes.InvalidArgument,
		"Program is required",
	)
	errNotAuthorised = status.Error(codes.PermissionDenied, "Not authorised")
)

var (
	// TODO: Make resource limits configurable via server config.
	defaultResourceLimits = &cgroups.ResourceLimits{
		CPUMaxPercent:  50,
		MemoryMaxBytes: 512 * 1024 * 1024, // 512 MB
		IOMaxBPS:       10 * 1024 * 1024,  // 10 MB/s
	}
)

type server struct {
	api.UnimplementedJobServiceServer

	manager    *jobmanager.Manager
	logger     *slog.Logger
	cfg        *config
	grpcServer *grpc.Server
	addr       string

	mu sync.Mutex
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

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			authUnaryInterceptor(s.logger),
		),
		grpc.ChainStreamInterceptor(
			authStreamInterceptor(s.logger),
		),
		grpc.Creds(credentials.NewTLS(tlsConfig)),
	)

	s.mu.Lock()
	s.grpcServer = grpcServer
	s.mu.Unlock()

	api.RegisterJobServiceServer(s.grpcServer, s)

	s.addr = listener.Addr().String()

	return s.grpcServer.Serve(listener)
}

func (s *server) shutdown() {
	s.mu.Lock()
	grpcServer := s.grpcServer
	s.mu.Unlock()

	if grpcServer == nil {
		s.logger.Warn("no gRPC server started")
		return
	}

	doneCh := make(chan struct{}, 1)
	go func() {
		grpcServer.GracefulStop()
		close(doneCh)
	}()

	select {
	case <-doneCh:
	case <-time.After(shutdownTimeout):
		s.logger.Warn("graceful shutdown timed out, forcing stop")
		grpcServer.Stop()
	}
}

// TODO: Add healthcheck methods for readiness/liveness probes.

func (s *server) RunJob(
	ctx context.Context,
	req *api.RunJobRequest,
) (*api.RunJobResponse, error) {
	if req.Program == "" {
		return nil, errProgramRequired
	}

	id, err := s.manager.RunJob(req.Program, req.Args, defaultResourceLimits)
	if err != nil {
		s.logger.Error(
			"failed to run job",
			"program", req.Program,
			"args", req.Args,
			"err", err,
		)
		return nil, s.mapError(err)
	}

	return &api.RunJobResponse{Id: id}, nil
}

func (s *server) StopJob(
	ctx context.Context,
	req *api.StopJobRequest,
) (*api.StopJobResponse, error) {
	if req.Id == "" {
		return nil, errIDRequired
	}

	if err := s.manager.StopJob(req.Id); err != nil {
		s.logger.Error(
			"failed to stop job",
			"id", req.Id,
			"err", err,
		)
		return nil, s.mapError(err)
	}

	return &api.StopJobResponse{}, nil
}

func (s *server) QueryJob(
	ctx context.Context,
	req *api.QueryJobRequest,
) (*api.QueryJobResponse, error) {
	if req.Id == "" {
		return nil, errIDRequired
	}

	jobStatus, err := s.manager.QueryJob(req.Id)
	if err != nil {
		s.logger.Error(
			"failed to query job",
			"id", req.Id,
			"err", err,
		)
		return nil, s.mapError(err)
	}

	var signal string
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
		return errIDRequired
	}

	outputReader, err := s.manager.StreamJobOutput(req.Id)
	if err != nil {
		s.logger.Error(
			"failed to stream job output",
			"id", req.Id,
			"err", err,
		)
		return s.mapError(err)
	}

	defer outputReader.Close()

	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	go func() {
		<-ctx.Done()
		outputReader.Close()
	}()

	buf := make([]byte, streamBufferSize)
	for {
		if stream.Context().Err() != nil {
			s.logger.Debug("stream cancelled by client", "id", req.Id)
			return status.FromContextError(stream.Context().Err()).Err()
		}

		n, err := outputReader.Read(buf)
		if n > 0 {
			output := bytes.Clone(buf[:n])
			if err := stream.Send(&api.StreamJobOutputResponse{
				Output: output,
			}); err != nil {
				s.logger.Warn("stream data to client", "id", req.Id, "err", err)
				return status.Error(codes.DataLoss, "failed to stream data")
			}
		}
		if err != nil {
			if err == io.EOF {
				s.logger.Debug("stream EOF", "id", req.Id)
				break
			}

			s.logger.Error(
				"unknown job streaming error",
				"id", req.Id,
				"err", err,
			)
			return s.mapError(err)
		}
	}

	return nil
}

// mapError translates jobmanager errors to gRPC errors.
func (s *server) mapError(err error) error {
	switch {
	case errors.Is(err, jobmanager.ErrJobNotFound):
		return status.Error(codes.NotFound, err.Error())

	case errors.As(err, new(jobmanager.InvalidStateError)):
		return status.Error(codes.FailedPrecondition, err.Error())

	default:
		return status.Error(codes.Internal, "Internal server error")
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
			return nil, errNotAuthorised
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
			return errNotAuthorised
		}

		return handler(srv, ss)
	}
}
