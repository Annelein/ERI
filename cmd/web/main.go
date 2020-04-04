package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"google.golang.org/api/option"

	"cloud.google.com/go/pubsub"

	"github.com/rs/cors"

	"github.com/Pimmr/rig"

	"github.com/Dynom/ERI/cmd/web/hitlist"
	"github.com/minio/highwayhash"

	"github.com/juju/ratelimit"

	"github.com/Dynom/ERI/validator"

	"github.com/Dynom/ERI/cmd/web/services"

	"github.com/Dynom/ERI/cmd/web/config"

	"github.com/Dynom/ERI/cmd/web/erihttp"

	"github.com/Dynom/ERI/cmd/web/erihttp/handlers"

	"github.com/Dynom/TySug/finder"
	"github.com/sirupsen/logrus"

	gqlHandler "github.com/graphql-go/handler"
)

// Version contains the app version, the value is changed during compile time to the appropriate Git tag
var Version = "dev"

func main() {
	var conf config.Config
	var err error

	conf, err = config.NewConfig("config.toml")
	if err != nil {
		panic(err)
	}

	err = rig.ParseStruct(&conf)
	if err != nil {
		panic(err)
	}

	var logger logrus.FieldLogger
	var logWriter *io.PipeWriter
	logger, logWriter, err = newLogger(conf)
	if err != nil {
		panic(err)
	}

	defer deferClose(logWriter, nil)

	logger = logger.WithField("version", Version)
	logger.WithFields(logrus.Fields{
		"config": conf,
	}).Info("Starting up...")

	h, err := highwayhash.New128([]byte(conf.Server.Hash.Key))
	if err != nil {
		panic(err)
	}

	psClient, err := pubsub.NewClient(
		context.Background(),
		conf.Server.GCP.ProjectID,
		option.WithUserAgent("eri-"+Version),
		option.WithCredentialsFile(conf.Server.GCP.CredentialsFile),
	)

	if err != nil {
		panic(err)
	}

	topic := psClient.Topic(conf.Server.GCP.PubSubTopic)
	if ok, err := topic.Exists(context.Background()); !ok || err != nil {
		logger.WithError(err).Errorf("Topic %q doesn't exist", conf.Server.GCP.PubSubTopic)
		panic(fmt.Sprintf("Topic %q doesn't exist", conf.Server.GCP.PubSubTopic))
	}

	hitList := hitlist.New(
		h,
		time.Hour*60, // @todo figure out what todo with TTLs
	)

	subscriptionLabel := `eri-` + Version + `-` + strconv.FormatInt(time.Now().Unix(), 10)
	logger.WithFields(logrus.Fields{
		"subscription_label": subscriptionLabel,
	}).Info("Creating subscription")

	subscription, err := psClient.CreateSubscription(
		context.Background(),
		subscriptionLabel,
		pubsub.SubscriptionConfig{
			Topic:               topic,
			AckDeadline:         time.Second * 600,
			RetainAckedMessages: false,
			ExpirationPolicy:    time.Hour * 25,
		},
	)

	if err != nil {
		panic(fmt.Sprintf("Failed to create subscription %s", err))
	}

	go func() {
		logger.WithField("subscription", subscription).Info("Subscription created, starting receiver...")
		err := subscription.Receive(context.Background(), func(ctx context.Context, message *pubsub.Message) {
			logger.WithFields(
				logrus.Fields{
					"msg_id": message.ID,
					"data":   string(message.Data),
				}).Info("Received something on the subscription")

			message.Ack()
		})

		if err != nil {
			logger.WithError(err).Warn("Error with pub/sub receivers")
		}
	}()

	myFinder, err := finder.New(
		hitList.GetValidAndUsageSortedDomains(),
		finder.WithLengthTolerance(0.2),
		finder.WithAlgorithm(finder.NewJaroWinklerDefaults()),
		finder.WithPrefixBuckets(conf.Server.Finder.UseBuckets),
	)

	if err != nil {
		panic(err)
	}

	var dialer = &net.Dialer{}
	if conf.Server.Validator.Resolver != "" {
		setCustomResolver(dialer, conf.Server.Validator.Resolver)
	}

	validationResultCache := &sync.Map{}
	validationResultPersister := &sync.Map{}

	val := validator.NewEmailAddressValidator(dialer)

	// Pick the validator we want to use
	checkValidator := mapValidatorTypeToValidatorFn(conf.Server.Validator.SuggestValidator, val)

	// Wrap it
	checkValidator = validatorPersistProxy(validationResultPersister, logger, checkValidator)
	checkValidator = validatorNotifyProxy(topic, hitList, logger, checkValidator)
	checkValidator = validatorUpdateFinderProxy(myFinder, hitList, logger, checkValidator)
	checkValidator = validatorCacheProxy(validationResultCache, logger, checkValidator)

	// Use it
	suggestSvc := services.NewSuggestService(myFinder, checkValidator, logger)

	mux := http.NewServeMux()
	healthHandler := NewHealthHandler(logger)
	mux.HandleFunc("/", healthHandler)
	mux.HandleFunc("/health", healthHandler)

	mux.HandleFunc("/suggest", NewSuggestHandler(logger, suggestSvc))
	mux.HandleFunc("/autocomplete", NewAutoCompleteHandler(logger, myFinder))

	schema, err := NewGraphQLSchema(&suggestSvc)
	if err != nil {
		logger.WithError(err).Error("Unable to build schema")
		os.Exit(1)
	}

	mux.Handle("/graph", gqlHandler.New(&gqlHandler.Config{
		Schema:     &schema,
		Pretty:     conf.Server.GraphQL.PrettyOutput,
		GraphiQL:   conf.Server.GraphQL.GraphiQL,
		Playground: conf.Server.GraphQL.Playground,
	}))

	if conf.Server.Profiler.Enable {
		configureProfiler(mux, conf)
	}

	// @todo status endpoint (or tick logger)

	var bucket *ratelimit.Bucket
	if conf.Server.RateLimiter.Rate > 0 && conf.Server.RateLimiter.Capacity > 0 {
		bucket = ratelimit.NewBucketWithRate(float64(conf.Server.RateLimiter.Rate), conf.Server.RateLimiter.Capacity)
	}

	ct := cors.New(cors.Options{
		AllowedOrigins: conf.Server.CORS.AllowedOrigins,
		AllowedHeaders: conf.Server.CORS.AllowedHeaders,
	})

	s := erihttp.BuildHTTPServer(mux, conf, logWriter,
		handlers.WithPathStrip(logger, conf.Server.PathStrip),
		handlers.NewRateLimitHandler(logger, bucket, conf.Server.RateLimiter.ParkedTTL.AsDuration()),
		handlers.WithRequestLogger(logger),
		handlers.WithGzipHandler(),
		handlers.WithHeaders(confHeadersToHTTPHeaders(conf.Server.Headers)),
		ct.Handler,
	)

	logger.WithFields(logrus.Fields{
		"listen_on": conf.Server.ListenOn,
	}).Info("Done, serving requests")
	err = s.ListenAndServe()

	logger.Errorf("HTTP server stopped %s", err)
}
