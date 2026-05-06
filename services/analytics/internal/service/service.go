// Package service implements the gRPC AnalyticsService.
package service

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/yld/url-shortener/services/analytics/internal/repo"
	analyticsv1 "github.com/yld/url-shortener/services/proto/gen/analytics/v1"
)

const dateLayout = "2006-01-02"

// Repo is the read contract used by Server.
type Repo interface {
	Stats(ctx context.Context, code string, from, to time.Time) (repo.Stats, error)
}

// Server implements analyticsv1.AnalyticsServiceServer.
type Server struct {
	analyticsv1.UnimplementedAnalyticsServiceServer
	repo Repo
}

// New constructs a Server.
func New(r Repo) *Server { return &Server{repo: r} }

// Stats implements AnalyticsService/Stats.
func (s *Server) Stats(ctx context.Context, req *analyticsv1.StatsRequest) (*analyticsv1.StatsResponse, error) {
	if req.GetCode() == "" {
		return nil, status.Error(codes.InvalidArgument, "code is required")
	}

	from, err := parseDate(req.GetFromDate())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "from_date: %v", err)
	}
	to, err := parseDate(req.GetToDate())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "to_date: %v", err)
	}
	if !from.IsZero() && !to.IsZero() && from.After(to) {
		return nil, status.Error(codes.InvalidArgument, "from_date must be <= to_date")
	}

	stats, err := s.repo.Stats(ctx, req.GetCode(), from, to)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "stats: %v", err)
	}

	resp := &analyticsv1.StatsResponse{
		Code:  req.GetCode(),
		Total: stats.Total,
	}
	if !stats.LastClickedAt.IsZero() {
		resp.LastClickedAt = timestamppb.New(stats.LastClickedAt)
	}
	for _, d := range stats.Daily {
		resp.Daily = append(resp.Daily, &analyticsv1.DailyCount{
			Date:  d.Date,
			Count: d.Count,
		})
	}
	return resp, nil
}

func parseDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}
