package deadletter

import (
	"context"
	"strings"
	"testing"
)

func TestParkRejectsMissingTenantBeforeWriting(t *testing.T) {
	var store Store
	err := store.Park(context.Background(), 0, "event-1", "topic", "created", "42", nil, "test")
	if err == nil || !strings.Contains(err.Error(), "tenant_id is required") {
		t.Fatalf("Park without a tenant returned %v; want tenant validation error", err)
	}
}
