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

type Server struct {
	api.UnimplementedJobServiceServer
	manager *jobmanager.Manager
}

func NewServer(manager *jobmanager.Manager) *Server {
	return &Server{manager: manager}
}

func runServer(config *serverConfig, manager *jobmanager.Manager) error {
	if config.debug {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", config.port))
	if err != nil {
		return fmt.Errorf("listen failed: %w", err)
	}

	grpcServer := NewServer(manager)

	// TODO: Set up credentials and middleware

	s := grpc.NewServer( /* creds and middleware */ )

	api.RegisterJobServiceServer(s, grpcServer)

	slog.Info("starting server", "port", config.port)

	// TODO: For a production system we'd have handling of signals and graceful
	// shutdown.
	return s.Serve(listener)
}

func (s *Server) RunJob(
	ctx context.Context,
	req *api.RunJobRequest,
) (*api.RunJobResponse, error) {
	slog.Debug("RunJob", "program", req.Program, "args", req.Args)

	id, err := s.manager.RunJob(req.Program, req.Args)
	if err != nil {
		slog.Error("failed to run job", "err", err)

		return nil, status.Error(codes.Internal, "Failed to run job")
	}

	slog.Debug("job started successfully", "job_id", id)

	return &api.RunJobResponse{Id: id}, nil
}

func (s *Server) StopJob(
	ctx context.Context,
	req *api.StopJobRequest,
) (*api.StopJobResponse, error) {
	slog.Debug("StopJob", "job_id", req.Id)

	if err := s.manager.StopJob(req.Id); err != nil {
		slog.Error("failed to stop job", "job_id", req.Id, "err", err)

		if errors.Is(err, jobmanager.ErrJobNotFound) {
			return nil, status.Error(codes.NotFound, "Job not found")
		}

		return nil, status.Error(codes.Internal, "Internal server error")
	}

	slog.Debug("job stopped successfully", "job_id", req.Id)

	return &api.StopJobResponse{}, nil
}

func (s *Server) QueryJob(
	ctx context.Context,
	req *api.QueryJobRequest,
) (*api.QueryJobResponse, error) {
	slog.Debug("QueryJob", "job_id", req.Id)

	jobStatus, err := s.manager.QueryJob(req.Id)
	if err != nil {
		slog.Error("failed to query job", "job_id", req.Id, "err", err)

		if errors.Is(err, jobmanager.ErrJobNotFound) {
			return nil, status.Error(codes.NotFound, "Job not found")
		}

		return nil, status.Error(codes.Internal, "Internal server error")
	}

	slog.Debug("job queried successfully", "job_id", req.Id)

	return &api.QueryJobResponse{
		State:       api.JobState(jobStatus.State),
		ExitCode:    int32(jobStatus.ExitCode),
		Signal:      jobStatus.Signal.String(),
		Interrupted: jobStatus.Interrupted,
	}, nil
}

func (s *Server) StreamJobOutput(
	req *api.StreamJobOutputRequest,
	stream api.JobService_StreamJobOutputServer,
) error {
	slog.Debug("StreamJobOutput", "job_id", req.Id)

	outputReader, err := s.manager.StreamJobOutput(req.Id)
	if err != nil {
		slog.Error(
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
			slog.Error(
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
				slog.Warn(
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

			slog.Warn(
				"error reading job output stream",
				"job_id",
				req.Id,
				"err",
				err,
			)

			return status.Error(codes.Internal, "Internal server error")
		}
	}

	slog.Debug("streamed job output successfully", "job_id", req.Id)

	return nil
}
