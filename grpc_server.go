// ABOUTME: gRPC server for DNS record management via protobuf.
// ABOUTME: Implements DynUpdateService with TLS support and auth interceptor.

package dynupdate

import (
	"context"
	"fmt"
	"net"

	pb "github.com/mauromedda/coredns-updater-plugin/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

// GRPCServer serves the gRPC management API.
type GRPCServer struct {
	store  *Store
	auth   *Auth
	listen string
	tls    *tlsConfig
	server *grpc.Server
}

// NewGRPCServer creates a gRPC server (not yet started).
func NewGRPCServer(store *Store, auth *Auth, listen string, tls *tlsConfig) *GRPCServer {
	return &GRPCServer{store: store, auth: auth, listen: listen, tls: tls}
}

// Start begins serving the gRPC API in a background goroutine.
func (g *GRPCServer) Start() error {
	ln, err := net.Listen("tcp", g.listen)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", g.listen, err)
	}

	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(g.auth.UnaryInterceptor),
	}

	if g.tls != nil {
		tlsCfg, err := buildTLSConfig(g.tls)
		if err != nil {
			ln.Close()
			return fmt.Errorf("building gRPC TLS config: %w", err)
		}
		opts = append(opts, grpc.Creds(credentials.NewTLS(tlsCfg)))
	}

	g.server = grpc.NewServer(opts...)
	pb.RegisterDynUpdateServiceServer(g.server, &grpcService{store: g.store})

	go func() {
		if err := g.server.Serve(ln); err != nil {
			log.Errorf("gRPC server error: %v", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the gRPC server.
func (g *GRPCServer) Stop() {
	if g.server == nil {
		return
	}
	g.server.GracefulStop()
}

// grpcService implements the DynUpdateService.
type grpcService struct {
	pb.UnimplementedDynUpdateServiceServer
	store *Store
}

func (s *grpcService) List(_ context.Context, req *pb.ListRequest) (*pb.ListResponse, error) {
	var records []Record
	if req.Name != "" {
		records = s.store.GetAll(req.Name)
	} else {
		records = s.store.List()
	}

	pbRecords := make([]*pb.Record, 0, len(records))
	for _, r := range records {
		pbRecords = append(pbRecords, recordToProto(r))
	}

	return &pb.ListResponse{Records: pbRecords}, nil
}

func (s *grpcService) Upsert(_ context.Context, req *pb.UpsertRequest) (*pb.UpsertResponse, error) {
	if req.Record == nil {
		return nil, status.Error(codes.InvalidArgument, "record is required")
	}

	rec := protoToRecord(req.Record)
	if err := rec.Validate(); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "validation failed: %v", err)
	}

	if err := s.store.Upsert(rec); err != nil {
		return nil, status.Errorf(codes.Internal, "upsert failed: %v", err)
	}

	return &pb.UpsertResponse{Record: recordToProto(rec)}, nil
}

func (s *grpcService) Delete(_ context.Context, req *pb.DeleteRequest) (*pb.DeleteResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	if req.Type == "" && req.Value == "" {
		if err := s.store.DeleteAll(req.Name); err != nil {
			return nil, status.Errorf(codes.Internal, "delete failed: %v", err)
		}
	} else {
		if err := s.store.Delete(req.Name, req.Type, req.Value); err != nil {
			return nil, status.Errorf(codes.Internal, "delete failed: %v", err)
		}
	}

	return &pb.DeleteResponse{}, nil
}

func recordToProto(r Record) *pb.Record {
	return &pb.Record{
		Name:     r.Name,
		Type:     r.Type,
		Ttl:      r.TTL,
		Value:    r.Value,
		Priority: uint32(r.Priority),
		Weight:   uint32(r.Weight),
		Port:     uint32(r.Port),
		Flag:     uint32(r.Flag),
		Tag:      r.Tag,
	}
}

func protoToRecord(p *pb.Record) Record {
	return Record{
		Name:     p.Name,
		Type:     p.Type,
		TTL:      p.Ttl,
		Value:    p.Value,
		Priority: uint16(p.Priority),
		Weight:   uint16(p.Weight),
		Port:     uint16(p.Port),
		Flag:     uint8(p.Flag),
		Tag:      p.Tag,
	}
}
