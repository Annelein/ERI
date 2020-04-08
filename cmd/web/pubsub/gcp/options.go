package gcp

type Option func(svc *PubSubSvc)

func WithSubscriptionLabels(labels []string) Option {
	return func(svc *PubSubSvc) {
		svc.subscriptionLabels = labels
	}
}

// WithSubscriptionConcurrencyCount Sets the concurrency count to GCPs subscription receiver
func WithSubscriptionConcurrencyCount(c int) Option {
	return func(svc *PubSubSvc) {
		svc.subscriptionNumProcs = c
	}
}
