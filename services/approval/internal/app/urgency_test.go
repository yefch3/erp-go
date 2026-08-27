package app

import (
	"testing"
	"time"
)

func TestApprovalUrgency(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		created  time.Time
		priority string
		minutes  int64
	}{
		{name: "normal", created: now.Add(-time.Hour), priority: priorityNormal, minutes: 71 * 60},
		{name: "high", created: now.Add(-25 * time.Hour), priority: priorityHigh, minutes: 47 * 60},
		{name: "urgent", created: now.Add(-49 * time.Hour), priority: priorityUrgent, minutes: 23 * 60},
		{name: "overdue", created: now.Add(-73 * time.Hour), priority: priorityOverdue, minutes: -60},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dueAt, priority, minutes := ApprovalUrgency(tt.created, now)
			if want := tt.created.Add(72 * time.Hour); !dueAt.Equal(want) {
				t.Fatalf("截止时间不正确: got %s want %s", dueAt, want)
			}
			if priority != tt.priority || minutes != tt.minutes {
				t.Fatalf("紧急程度不正确: priority=%s minutes=%d", priority, minutes)
			}
		})
	}
}
