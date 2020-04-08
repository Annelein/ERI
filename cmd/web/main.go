package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/Dynom/ERI/cmd/web/pubsub/gcp"
	"github.com/Dynom/ERI/runtimer"
	"google.golang.org/api/option"

	gcppubsub "cloud.google.com/go/pubsub"

	"github.com/rs/cors"

	"github.com/Pimmr/rig"

	"github.com/Dynom/ERI/cmd/web/hitlist"
	"github.com/minio/highwayhash"

	"github.com/juju/ratelimit"

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
	if conf.Server.InstanceID != "" {
		logger = logger.WithField("instance_id", conf.Server.InstanceID)
	}

	logger.WithFields(logrus.Fields{
		"config": conf,
	}).Info("Starting up...")

	h, err := highwayhash.New128([]byte(conf.Server.Hash.Key))
	if err != nil {
		logger.WithError(err).Error("Unable to create our hash.Hash")
		os.Exit(1)
	}

	psClientCtx, psClientCtxCancel := context.WithCancel(context.Background())
	psClient, err := gcppubsub.NewClient(
		psClientCtx,
		conf.Server.GCP.ProjectID,
		option.WithUserAgent("eri-"+Version),
		option.WithCredentialsFile(conf.Server.GCP.CredentialsFile),
	)

	if err != nil {
		logger.WithError(err).Error("Unable to create the pub/sub client")
		os.Exit(1)
	}

	psSvc := gcp.NewPubSubSvc(
		logger,
		psClient,
		conf.Server.GCP.PubSubTopic,
		gcp.WithSubscriptionLabels([]string{Version, conf.Server.InstanceID, strconv.FormatInt(time.Now().Unix(), 10)}),
		gcp.WithSubscriptionConcurrencyCount(5),
	)

	// Setting up listening to notifications
	pubSubCtx, cancel := context.WithCancel(context.Background())

	rt := runtimer.New(os.Interrupt, os.Kill)
	rt.RegisterCallback(func(s os.Signal) {
		logger.Printf("Captured signal: %v. Starting cleanup", s)
		logger.Debug("Canceling pub/sub context")
		cancel()
	})

	rt.RegisterCallback(func(s os.Signal) {
		logger.Debug("Closing Pub/Sub service")
		deferClose(psSvc, logger)
	})

	rt.RegisterCallback(func(s os.Signal) {
		logger.Debug("Canceling GCP client context")
		psClientCtxCancel()
	})

	rt.RegisterCallback(func(_ os.Signal) {
		os.Exit(0)
	})

	hitList := hitlist.New(
		h,
		time.Hour*60, // @todo figure out what todo with TTLs
	)

	myFinder, err := finder.New(
		hitList.GetValidAndUsageSortedDomains(),
		finder.WithLengthTolerance(0.2),
		finder.WithAlgorithm(finder.NewJaroWinklerDefaults()),
		finder.WithPrefixBuckets(conf.Server.Finder.UseBuckets),
	)

	if err != nil {
		logger.WithError(err).Error("Unable to create Finder")
		os.Exit(1)
	}

	logger.Debug("Starting listener...")
	err = psSvc.Listen(pubSubCtx, pubSubNotificationHandler(hitList, logger, myFinder))

	if err != nil {
		logger.WithError(err).Error("Failed constructing pub/sub client")
		os.Exit(1)
	}

	// @todo add a real persisting layer
	validationResultPersister := &sync.Map{}

	validatorFn := createProxiedValidator(conf, logger, hitList, myFinder, psSvc, validationResultPersister)
	suggestSvc := services.NewSuggestService(myFinder, validatorFn, logger)

	mux := http.NewServeMux()
	registerProfileHandler(mux, conf)
	registerHealthHandler(mux, logger)

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
