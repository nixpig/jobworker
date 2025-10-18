package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"

	api "github.com/nixpig/jobworker/api/v1"
	"github.com/nixpig/jobworker/internal/jobmanager"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	streamBufferSize = 4096 // 4KB
)

// TODO: Generalise error handling in handlers to remove duplication.

type server struct {
	api.UnimplementedJobServiceServer

	manager    *jobmanager.Manager
	logger     *slog.Logger
	cfg        *config
	grpcServer *grpc.Server
}

func newServer(
	manager *jobmanager.Manager,
	logger *slog.Logger,
	cfg *config,
) *server {
	return &server{manager: manager, logger: logger, cfg: cfg}
}

func (s *server) start() error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.cfg.port))
	if err != nil {
		return fmt.Errorf("listen failed: %w", err)
	}

	// TODO: Set up credentials and middleware

	s.grpcServer = grpc.NewServer( /* creds and middleware */ )

	api.RegisterJobServiceServer(s.grpcServer, s)

	return s.grpcServer.Serve(listener)
}

func (s *server) shutdown() {
	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}
}

func (s *server) RunJob(
	ctx context.Context,
	req *api.RunJobRequest,
) (*api.RunJobResponse, error) {
	s.logger.Debug("RunJob", "program", req.Program, "args", req.Args)

	if ctx.Err() != nil {
		return nil, status.FromContextError(ctx.Err()).Err()
	}

	if req.Program == "" {
		return nil, status.Error(
			codes.InvalidArgument,
			"program cannot be empty",
		)
	}

	id, err := s.manager.RunJob(req.Program, req.Args)
	if err != nil {
		s.logger.Error("failed to run job", "err", err)

		return nil, status.Error(codes.Internal, "Failed to run job")
	}

	s.logger.Debug("job started successfully", "job_id", id)

	return &api.RunJobResponse{Id: id}, nil
}

func (s *server) StopJob(
	ctx context.Context,
	req *api.StopJobRequest,
) (*api.StopJobResponse, error) {
	s.logger.Debug("StopJob", "job_id", req.Id)

	if ctx.Err() != nil {
		return nil, status.FromContextError(ctx.Err()).Err()
	}

	if err := s.manager.StopJob(req.Id); err != nil {
		s.logger.Error("failed to stop job", "job_id", req.Id, "err", err)

		if errors.Is(err, jobmanager.ErrJobNotFound) {
			return nil, status.Error(codes.NotFound, "Job not found")
		}

		return nil, status.Error(codes.Internal, "Internal server error")
	}

	s.logger.Debug("job stopped successfully", "job_id", req.Id)

	return &api.StopJobResponse{}, nil
}

func (s *server) QueryJob(
	ctx context.Context,
	req *api.QueryJobRequest,
) (*api.QueryJobResponse, error) {
	s.logger.Debug("QueryJob", "job_id", req.Id)

	if ctx.Err() != nil {
		return nil, status.FromContextError(ctx.Err()).Err()
	}

	jobStatus, err := s.manager.QueryJob(req.Id)
	if err != nil {
		s.logger.Error("failed to query job", "job_id", req.Id, "err", err)

		if errors.Is(err, jobmanager.ErrJobNotFound) {
			return nil, status.Error(codes.NotFound, "Job not found")
		}

		return nil, status.Error(codes.Internal, "Internal server error")
	}

	s.logger.Debug("job queried successfully", "job_id", req.Id)

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
	s.logger.Debug("StreamJobOutput", "job_id", req.Id)

	if stream.Context().Err() != nil {
		return status.FromContextError(stream.Context().Err()).Err()
	}

	outputReader, err := s.manager.StreamJobOutput(req.Id)
	if err != nil {
		s.logger.Error(
			"failed to get job output stream",
			"job_id",
			req.Id,
			"err",
			err,
		)

		if errors.Is(err, jobmanager.ErrJobNotFound) {
			return status.Error(codes.NotFound, "Job not found")
		}

		return status.Error(codes.Internal, "Internal server error")
	}

	defer func() {
		if err := outputReader.Close(); err != nil {
			s.logger.Error(
				"failed to close output reader",
				"job_id",
				req.Id,
				"err",
				err,
			)
		}
	}()

	buf := make([]byte, streamBufferSize)
	for {
		n, err := outputReader.Read(buf)
		if n > 0 {
			if err := stream.Send(&api.StreamJobOutputResponse{
				Output: buf[:n],
			}); err != nil {
				s.logger.Warn(
					"failed to stream data to client",
					"job_id",
					req.Id,
					"err",
					err,
				)

				return status.Error(codes.DataLoss, "Failed to stream data")
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}

			s.logger.Warn(
				"error reading job output stream",
				"job_id",
				req.Id,
				"err",
				err,
			)

			return status.Error(codes.Internal, "Internal server error")
		}
	}

	s.logger.Debug("streamed job output successfully", "job_id", req.Id)

	return nil
}
