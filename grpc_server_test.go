// ABOUTME: Tests for the gRPC server: List, Upsert, Delete RPCs with auth.
// ABOUTME: Uses in-process gRPC connections for fast testing without network.

package dynupdate

import (
	"context"
	"net"
	"path/filepath"
	"testing"

	pb "github.com/mauromedda/coredns-updater-plugin/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

func newTestGRPCClient(t *testing.T, token string) (pb.DynUpdateServiceClient, *Store) {
	t.Helper()
	dir := t.TempDir()
	fp := filepath.Join(dir, "records.json")

	store, err := NewStore(fp, 0)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	t.Cleanup(func() { store.Stop() })

	auth := &Auth{Token: "grpc-secret"}
	srv := grpc.NewServer(grpc.UnaryInterceptor(auth.UnaryInterceptor))
	pb.RegisterDynUpdateServiceServer(srv, &grpcService{store: store})

	lis := bufconn.Listen(bufSize)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() { srv.GracefulStop() })

	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	return pb.NewDynUpdateServiceClient(conn), store
}

func authCtx(token string) context.Context {
	md := metadata.Pairs("authorization", "Bearer "+token)
	return metadata.NewOutgoingContext(context.Background(), md)
}

func TestGRPC_List_Empty(t *testing.T) {
	t.Parallel()
	client, _ := newTestGRPCClient(t, "grpc-secret")

	resp, err := client.List(authCtx("grpc-secret"), &pb.ListRequest{})
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(resp.Records) != 0 {
		t.Errorf("got %d records, want 0", len(resp.Records))
	}
}

func TestGRPC_UpsertAndList(t *testing.T) {
	t.Parallel()
	client, _ := newTestGRPCClient(t, "grpc-secret")
	ctx := authCtx("grpc-secret")

	// Upsert
	_, err := client.Upsert(ctx, &pb.UpsertRequest{
		Record: &pb.Record{
			Name:  "app.example.org.",
			Type:  "A",
			Ttl:   300,
			Value: "10.0.0.1",
		},
	})
	if err != nil {
		t.Fatalf("Upsert() error: %v", err)
	}

	// List
	resp, err := client.List(ctx, &pb.ListRequest{})
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(resp.Records) != 1 {
		t.Fatalf("got %d records, want 1", len(resp.Records))
	}
	if resp.Records[0].Value != "10.0.0.1" {
		t.Errorf("Value = %q, want %q", resp.Records[0].Value, "10.0.0.1")
	}
}

func TestGRPC_ListByName(t *testing.T) {
	t.Parallel()
	client, store := newTestGRPCClient(t, "grpc-secret")
	_ = store.Upsert(Record{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"})
	_ = store.Upsert(Record{Name: "other.example.org.", Type: "A", TTL: 300, Value: "10.0.0.2"})

	resp, err := client.List(authCtx("grpc-secret"), &pb.ListRequest{Name: "app.example.org."})
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(resp.Records) != 1 {
		t.Errorf("got %d records, want 1", len(resp.Records))
	}
}

func TestGRPC_Delete(t *testing.T) {
	t.Parallel()
	client, store := newTestGRPCClient(t, "grpc-secret")
	_ = store.Upsert(Record{Name: "app.example.org.", Type: "A", TTL: 300, Value: "10.0.0.1"})
	ctx := authCtx("grpc-secret")

	_, err := client.Delete(ctx, &pb.DeleteRequest{
		Name:  "app.example.org.",
		Type:  "A",
		Value: "10.0.0.1",
	})
	if err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	resp, err := client.List(ctx, &pb.ListRequest{})
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(resp.Records) != 0 {
		t.Errorf("got %d records after delete, want 0", len(resp.Records))
	}
}

func TestGRPC_Unauthenticated(t *testing.T) {
	t.Parallel()
	client, _ := newTestGRPCClient(t, "grpc-secret")

	// No auth metadata
	_, err := client.List(context.Background(), &pb.ListRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	s, ok := status.FromError(err)
	if !ok || s.Code() != codes.Unauthenticated {
		t.Errorf("code = %v, want Unauthenticated", err)
	}
}

func TestGRPC_Upsert_ValidationError(t *testing.T) {
	t.Parallel()
	client, _ := newTestGRPCClient(t, "grpc-secret")

	_, err := client.Upsert(authCtx("grpc-secret"), &pb.UpsertRequest{
		Record: &pb.Record{
			Name:  "bad-name",
			Type:  "A",
			Ttl:   300,
			Value: "10.0.0.1",
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid record")
	}
	s, ok := status.FromError(err)
	if !ok || s.Code() != codes.InvalidArgument {
		t.Errorf("code = %v, want InvalidArgument", err)
	}
}
