package gcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	gcppubsub "cloud.google.com/go/pubsub"
	"github.com/Dynom/ERI/cmd/web/erihttp/handlers"
	"github.com/Dynom/ERI/cmd/web/pubsub"
	"github.com/sirupsen/logrus"
)

func NewPubSubSvc(logger logrus.FieldLogger, client *gcppubsub.Client, topicName string, options ...Option) *PubSubSvc {
	if logger == nil {
		logger = logrus.New()
	}

	svc := PubSubSvc{
		logger:    logger,
		client:    client,
		topicName: topicName,
	}

	for _, o := range options {
		o(&svc)
	}

	labels := svc.subscriptionLabels
	svc.subscriptionLabels = []string{"eri"}
	for _, l := range labels {
		if l == "" {
			continue
		}
		svc.subscriptionLabels = append(svc.subscriptionLabels, l)
	}

	return &svc
}

type NotifyFn func(ctx context.Context, notification pubsub.Notification)

type PubSubSvc struct {
	logger               logrus.FieldLogger
	client               *gcppubsub.Client
	topicName            string
	topic                *gcppubsub.Topic
	subscriptionLabels   []string
	subscriptionNumProcs int
}

func (svc PubSubSvc) Close() error {
	if svc.client == nil {
		return errors.New("client not defined")
	}
	err := svc.cleanupSubscription(svc.client.Subscription(svc.getSubscriptionID()))
	if err != nil {
		svc.logger.WithError(err).Warn("Failed to end and cleanup subscription")
	}
	_ = svc.client.Close()
	return err
}

func (svc PubSubSvc) cleanupSubscription(subscription *gcppubsub.Subscription) error {
	if subscription == nil {
		return nil
	}

	return subscription.Delete(context.Background())
}

func (svc PubSubSvc) getSubscriptionID() string {
	return strings.Join(svc.subscriptionLabels, `-`)
}

func (svc *PubSubSvc) Publish(ctx context.Context, data pubsub.Data) error {

	logger := svc.logger.WithFields(logrus.Fields{
		handlers.RequestID.String(): ctx.Value(handlers.RequestID),
	})

	// @todo Current design is chatty. Do we need to support batching and publish at earliest: interval / max payload size?

	var notification = pubsub.Notification{
		SenderID: svc.getSubscriptionID(),
		Data:     data,
	}

	payload, err := json.Marshal(notification)
	if err != nil {
		logger.WithFields(logrus.Fields{
			"error":        err,
			"notification": notification,
		}).Error("Failed to marshal notification")
		return err
	}

	topic, err := svc.getTopic(ctx, svc.topicName)
	if err != nil {
		return err
	}

	pr := topic.Publish(ctx, &gcppubsub.Message{
		Data: payload,
	})

	<-pr.Ready()

	return nil
}

// getTopic returns the topic, after verifying it exists. Multiple calls to getTopic will return the same topic
func (svc *PubSubSvc) getTopic(ctx context.Context, topicName string) (*gcppubsub.Topic, error) {
	if svc.topic != nil {
		return svc.topic, nil
	}

	topic := svc.client.Topic(topicName)
	if ok, err := topic.Exists(ctx); !ok || err != nil {
		return nil, err
	}

	svc.topic = topic

	return svc.topic, nil

}

func (svc *PubSubSvc) maintainSubscription(ctx context.Context, fn NotifyFn, topic *gcppubsub.Topic) {
	var subscription *gcppubsub.Subscription

	subscriptionID := svc.getSubscriptionID()

	var attempts uint64
	var retries = -1
	var lastErrorTime = time.Time{}
	for {
		var err error
		attempts++
		retries++

		if retries >= 100 {
			svc.logger.WithFields(logrus.Fields{
				"attempt": attempts,
				"retries": retries,
			}).Warn("Giving up on receiving notifications. We're in a broken state!")
			return
		}

		if ctx.Err() != nil {
			svc.logger.WithFields(logrus.Fields{
				"attempt": attempts,
				"ctx_err": ctx.Err(),
			}).Warn("Context canceled, giving up")
			return
		}

		svc.logger.Debug("Creating subscription")
		subscription, err = svc.createSubscription(ctx, topic, subscriptionID)
		if err != nil {

			if !strings.Contains(err.Error(), "AlreadyExists") {
				svc.logger.WithFields(logrus.Fields{
					"error":              err.Error(),
					"topic":              topic,
					"subscription":       subscription,
					"subscription_label": subscriptionID,
				}).Error("Failed to setup subscription for this project")
				continue
			}

			svc.logger.WithFields(logrus.Fields{
				"error":           err.Error(),
				"subscription_id": subscriptionID,
			}).Warn("Subscription already exists, creating a reference to the existing one")

			subscription = svc.client.Subscription(subscriptionID)
			lastErrorTime = time.Now()
		}

		subscription.ReceiveSettings.NumGoroutines = svc.subscriptionNumProcs
		err = svc.listen(ctx, subscription, fn)
		if err != nil {
			if shouldResetRetries(lastErrorTime, retries) {
				retries = 0
			}

			if strings.Contains(err.Error(), "NotFound") {

				// There can be an inconsistency between a created subscription and one that isn't ready for receiving notifications
				// the underlying RPC can reply with both AlreadyExists and NotFound errors on the same subscription.
				if exists, existsErr := subscription.Exists(ctx); exists && existsErr == nil {
					sleepyTime(lastErrorTime, retries, func(t time.Duration) {
						svc.logger.WithFields(logrus.Fields{
							"error":            err,
							"subscription_err": existsErr,
							"retries":          retries,
							"sleepy_time":      t,
						}).Debug("In 'eventually consistent' limbo on GCP. Resource both exists and doesn't exist. " +
							"Sleeping before retrying a call to Receive again")
					})

					lastErrorTime = time.Now()
					continue
				}

				svc.logger.WithFields(logrus.Fields{
					"error":    err,
					"attempts": attempts,
					"retries":  retries,
				}).Error("Subscription not available, did it got deleted?")

				lastErrorTime = time.Now()
				continue
			}

			svc.logger.WithFields(logrus.Fields{
				"error":    err,
				"attempts": attempts,
				"retries":  retries,
			}).Error("Error with pub/sub receivers.")
			lastErrorTime = time.Now()
		}
	}
}

// Listen uses the client co connect to GCP and attach to a Topic, while attaching a new unique subscriber for
// receiving notifications. Listen returns immediately
func (svc *PubSubSvc) Listen(ctx context.Context, fn NotifyFn) error {

	topic, err := svc.getTopic(ctx, svc.topicName)
	if err != nil {
		svc.logger.WithFields(logrus.Fields{
			"error": err,
			"topic": svc.topicName,
		}).Error("Topic not found for this project")
		return err
	}

	go svc.maintainSubscription(ctx, fn, topic)

	return nil
}

func (svc *PubSubSvc) createSubscription(ctx context.Context, topic *gcppubsub.Topic, sid string) (*gcppubsub.Subscription, error) {
	s, err := svc.client.CreateSubscription(
		ctx,
		sid,
		gcppubsub.SubscriptionConfig{
			Topic:               topic,
			AckDeadline:         time.Second * 600,
			RetainAckedMessages: false,
			ExpirationPolicy:    time.Hour * 25,
		},
	)

	if err != nil {
		return nil, err
	}

	return s, nil
}

// listen listens on a subscription
func (svc *PubSubSvc) listen(ctx context.Context, subscription Subscription, fn NotifyFn) error {
	if subscription == nil {
		return errors.New("invalid subscription")
	}

	if exists, err := subscription.Exists(ctx); !exists {
		svc.logger.WithField("subscription_id", subscription.ID()).Info("Subscription doesn't exist, unable to start receiver.")
		return err
	}

	svc.logger.WithField("subscription", subscription).Info("Starting receiver on subscription.")

	receivedLock := sync.Mutex{}
	ignoredLock := sync.Mutex{}
	var received uint
	var ignored uint
	return subscription.Receive(ctx, func(ctx context.Context, message *gcppubsub.Message) {

		receivedLock.Lock()
		received++
		numSeen := received
		receivedLock.Unlock()

		logger := svc.logger.WithFields(logrus.Fields{
			"msg_id":             message.ID,
			"notifications_seen": numSeen,
		})

		var notification pubsub.Notification

		err := json.Unmarshal(message.Data, &notification)
		if err != nil {
			logger.WithFields(logrus.Fields{
				"error": err,
				"data":  string(message.Data),
			}).Warn("Unable to unmarshal notification")

			message.Nack()
			return
		}

		message.Ack()

		// Making sure we don't respond to our own publishing
		if sid := svc.getSubscriptionID(); notification.SenderID == sid {
			ignoredLock.Lock()
			ignored++
			numIgnored := ignored
			ignoredLock.Unlock()

			logger.WithFields(logrus.Fields{
				"sender_id":             notification.SenderID,
				"subscription_id":       sid,
				"notifications_ignored": numIgnored,
			}).Debug("Ignoring notification sent by this instance.")
			return
		}

		// Calling cb
		fn(ctx, notification)
	})

}

// sleepyTime sleeps up to a max of (retries * time.Second), depending on how long it has been since time t
// typical usage is: sleepyTime(lastErrorTime, retries, func(t time.Duration) { log.Logf("Slept for %q", t) )
func sleepyTime(t time.Time, retries int, fn func(t time.Duration)) {
	d := getDurationToSleep(t, retries)
	fn(d)

	time.Sleep(d)
}

func shouldResetRetries(t time.Time, retries int) bool {
	return getDurationToSleep(t, retries) < 0
}

func getDurationToSleep(t time.Time, retries int) time.Duration {

	retries *= 2
	if retries <= 0 {
		retries = 1
	}

	return time.Until(t.Add(time.Second * time.Duration(retries*2)))
}
