package pilot

import (
	"context"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/contract"
)

type stubServices struct {
	list   []collector.ServiceInfo
	detail map[string]*collector.ServiceDetail
}

func (s *stubServices) CollectServices(_ context.Context) []collector.ServiceInfo {
	return s.list
}
func (s *stubServices) CollectServiceDetail(_ context.Context, name string) *collector.ServiceDetail {
	return s.detail[name]
}

func TestServicesListHandler(t *testing.T) {
	stub := &stubServices{list: []collector.ServiceInfo{{Name: "db", Status: "healthy"}, {Name: "cache", Status: "degraded"}}}
	h := servicesListHandler(stub)
	res, err := h(context.Background(), struct{}{}, contract.Principal{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if len(res.Services) != 2 {
		t.Errorf("services = %d", len(res.Services))
	}
}

func TestServicesDetailHandler_Found(t *testing.T) {
	stub := &stubServices{detail: map[string]*collector.ServiceDetail{
		"db": {Name: "db", Type: "postgres"},
	}}
	h := servicesDetailHandler(stub)
	res, err := h(context.Background(), ServiceDetailInput{Name: "db"}, contract.Principal{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if res == nil || res.Name != "db" {
		t.Errorf("detail = %+v", res)
	}
}

func TestServicesDetailHandler_NotFound(t *testing.T) {
	stub := &stubServices{detail: map[string]*collector.ServiceDetail{}}
	h := servicesDetailHandler(stub)
	_, err := h(context.Background(), ServiceDetailInput{Name: "missing"}, contract.Principal{})
	if err == nil {
		t.Fatal("expected not-found")
	}
	ce, ok := err.(*contract.Error)
	if !ok || ce.Code != contract.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", err)
	}
}

func TestServicesDetailHandler_EmptyNameIsBadRequest(t *testing.T) {
	stub := &stubServices{}
	h := servicesDetailHandler(stub)
	_, err := h(context.Background(), ServiceDetailInput{Name: ""}, contract.Principal{})
	if err == nil {
		t.Fatal("expected bad-request")
	}
	ce, ok := err.(*contract.Error)
	if !ok || ce.Code != contract.CodeBadRequest {
		t.Errorf("expected CodeBadRequest, got %v", err)
	}
}
