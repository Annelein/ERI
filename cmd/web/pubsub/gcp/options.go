package gcp

type Option func(svc *PubSubSvc)

func WithSubscriptionLabels(labels []string) Option {
	return func(svc *PubSubSvc) {
		svc.subscriptionLabels = labels
	}
}
