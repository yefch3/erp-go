// Package grpcout holds the clients notification uses to reach other
// services. It keeps no copy of their data.
package grpcout

import (
	"context"
	"io"
	"time"

	"google.golang.org/grpc"

	iamv1 "github.com/sgao19/erp-go/gen/go/erp/iam/v1"
	mdv1 "github.com/sgao19/erp-go/gen/go/erp/masterdata/v1"
	"github.com/sgao19/erp-go/pkg/blobstore"
	"github.com/sgao19/erp-go/services/notification/internal/app"
)

// Numbering issues campaign numbers. Notification runs no sequence of its
// own: one service owns them all.
type Numbering struct{ client mdv1.NumberingServiceClient }

func NewNumbering(conn *grpc.ClientConn) *Numbering {
	return &Numbering{client: mdv1.NewNumberingServiceClient(conn)}
}

func (n *Numbering) Next(ctx context.Context, bizType string) (string, error) {
	resp, err := n.client.NextNumber(ctx, &mdv1.NextNumberRequest{BizType: bizType})
	if err != nil {
		return "", err
	}
	return resp.GetNumber(), nil
}

// Directory reads the sender's details so signature variables resolve.
type Directory struct{ client iamv1.DirectoryServiceClient }

func NewDirectory(conn *grpc.ClientConn) *Directory {
	return &Directory{client: iamv1.NewDirectoryServiceClient(conn)}
}

func (d *Directory) Get(ctx context.Context, employeeID int64) (app.Employee, error) {
	resp, err := d.client.GetEmployee(ctx, &iamv1.GetEmployeeRequest{Id: employeeID})
	if err != nil {
		return app.Employee{}, err
	}
	e := resp.GetEmployee()
	return app.Employee{
		ID: e.GetId(), Name: e.GetName(), Title: e.GetPosition(),
		Email: e.GetEmail(), Phone: e.GetPhone(),
	}, nil
}

// Scopes asks iam whose correspondence the caller may read. Reading somebody
// else's mail is a real act, so the rule lives with the organisation chart
// rather than being reinvented here.
type Scopes struct{ client iamv1.AccessServiceClient }

func NewScopes(conn *grpc.ClientConn) *Scopes {
	return &Scopes{client: iamv1.NewAccessServiceClient(conn)}
}

func (s *Scopes) VisibleEmployees(ctx context.Context, employeeID int64, module string) (app.Visibility, error) {
	resp, err := s.client.VisibleEmployees(ctx, &iamv1.VisibleEmployeesRequest{
		EmployeeId: employeeID, Module: module,
	})
	if err != nil {
		return app.Visibility{}, err
	}
	return app.Visibility{
		All: resp.GetAll(), EmployeeIDs: resp.GetEmployeeIds(), ScopeType: resp.GetScopeType(),
	}, nil
}

// Files adapts blobstore to the app's Files port. Upload URLs are short-lived
// because they are a capability: whoever holds one can write that object.
type Files struct {
	store     *blobstore.Store
	putExpiry time.Duration
}

func NewFiles(store *blobstore.Store) *Files {
	return &Files{store: store, putExpiry: 10 * time.Minute}
}

func (f *Files) PresignPut(ctx context.Context, key string) (string, int32, error) {
	u, err := f.store.PresignedPut(ctx, key, f.putExpiry)
	if err != nil {
		return "", 0, err
	}
	return u.String(), int32(f.putExpiry.Seconds()), nil
}

func (f *Files) Stat(ctx context.Context, key string) (int64, string, error) {
	return f.store.Stat(ctx, key)
}

func (f *Files) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return f.store.Get(ctx, key)
}

func (f *Files) Remove(ctx context.Context, key string) error {
	return f.store.Remove(ctx, key)
}
