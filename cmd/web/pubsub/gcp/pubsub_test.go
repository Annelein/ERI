package gcp

import (
	"testing"
)

func TestPubSubSvc_getSubscriptionID(t *testing.T) {
	tests := []struct {
		name   string
		labels []string
		want   string
	}{
		{name: "no label", labels: []string{}, want: "eri"},
		{name: "single label", labels: []string{"a"}, want: "eri-a"},
		{name: "multi label", labels: []string{"a", "b"}, want: "eri-a-b"},
		{name: "multi label skip empty", labels: []string{"a", "", "b"}, want: "eri-a-b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewPubSubSvc(nil, nil, "", WithSubscriptionLabels(tt.labels))

			if got := svc.getSubscriptionID(); got != tt.want {
				t.Errorf("getSubscriptionID() = %v, want %v", got, tt.want)
			}
		})
	}
}
