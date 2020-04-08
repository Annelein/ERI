package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"sync"

	"github.com/Dynom/ERI/cmd/web/hitlist"
	"github.com/Dynom/ERI/cmd/web/pubsub"
	"github.com/Dynom/ERI/cmd/web/pubsub/gcp"
	"github.com/Dynom/ERI/types"
	"github.com/Dynom/ERI/validator"
	"github.com/Dynom/TySug/finder"

	"github.com/sirupsen/logrus"

	"github.com/Dynom/ERI/cmd/web/config"
)

func confHeadersToHTTPHeaders(ch config.Headers) http.Header {
	headers := http.Header{}
	for h, v := range ch {
		headers.Add(h, v)
	}

	return headers
}

func newLogger(conf config.Config) (*logrus.Logger, *io.PipeWriter, error) {
	var err error
	logger := logrus.New()

	if conf.Server.Log.Format == config.LFJSON {
		logger.SetFormatter(&logrus.JSONFormatter{})
	} else {
		logger.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: `15:04:05.999999`,
		})
	}

	logger.SetOutput(os.Stdout)
	level, err := logrus.ParseLevel(conf.Server.Log.Level)
	if err == nil {
		logger.SetLevel(level)
	}

	return logger, logger.WriterLevel(level), err
}

func registerProfileHandler(mux *http.ServeMux, conf config.Config) {

	if !conf.Server.Profiler.Enable {
		return
	}

	var prefix = "debug"
	if conf.Server.Profiler.Prefix != "" {
		prefix = conf.Server.Profiler.Prefix
	}

	mux.HandleFunc(`/`+prefix+`/pprof/`, pprof.Index)
	mux.HandleFunc(`/`+prefix+`/pprof/cmdline`, pprof.Cmdline)
	mux.HandleFunc(`/`+prefix+`/pprof/profile`, pprof.Profile)
	mux.HandleFunc(`/`+prefix+`/pprof/symbol`, pprof.Symbol)
	mux.HandleFunc(`/`+prefix+`/pprof/trace`, pprof.Trace)
}

func setCustomResolver(dialer *net.Dialer, host string) {
	if dialer.Resolver == nil {
		dialer.Resolver = &net.Resolver{
			PreferGo: true,
		}
	}

	dialer.Resolver.Dial = func(ctx context.Context, network, address string) (conn net.Conn, e error) {
		d := net.Dialer{}
		return d.DialContext(ctx, network, net.JoinHostPort(host, `53`))
	}
}

func deferClose(toClose io.Closer, log logrus.FieldLogger) {
	if toClose == nil {
		return
	}

	err := toClose.Close()
	if err != nil {
		if log == nil {
			fmt.Printf("error failed to close handle %s", err)
			return
		}

		log.WithError(err).Error("Failed to close handle")
	}
}

func mapValidatorTypeToValidatorFn(vt config.ValidatorType, v validator.EmailValidator) validator.CheckFn {
	switch vt {
	case config.VTLookup:
		return v.CheckWithLookup
	case config.VTStructure:
		return v.CheckWithSyntax
	}

	panic(fmt.Sprintf("Incorrect validator %q configured.", vt))
}

func createProxiedValidator(conf config.Config, logger logrus.FieldLogger, hitList *hitlist.HitList, myFinder *finder.Finder, psSvc *gcp.PubSubSvc, validationResultPersister *sync.Map) validator.CheckFn {
	var dialer = &net.Dialer{}
	if conf.Server.Validator.Resolver != "" {
		setCustomResolver(dialer, conf.Server.Validator.Resolver)
	}

	val := validator.NewEmailAddressValidator(dialer)

	// Pick the validator we want to use
	checkValidator := mapValidatorTypeToValidatorFn(conf.Server.Validator.SuggestValidator, val)

	checkValidator = validatorPersistProxy(validationResultPersister, hitList, logger, checkValidator)
	checkValidator = validatorNotifyProxy(psSvc, hitList, logger, checkValidator)
	checkValidator = validatorUpdateFinderProxy(myFinder, hitList, logger, checkValidator)
	checkValidator = validatorHitListProxy(hitList, logger, checkValidator)

	return checkValidator

}

func registerHealthHandler(mux *http.ServeMux, logger logrus.FieldLogger) {
	healthHandler := NewHealthHandler(logger)

	mux.HandleFunc("/", healthHandler)
	mux.HandleFunc("/health", healthHandler)
}

func pubSubNotificationHandler(hitList *hitlist.HitList, logger logrus.FieldLogger, myFinder *finder.Finder) gcp.NotifyFn {

	logger = logger.WithField("handler", "notification")
	return func(ctx context.Context, notification pubsub.Notification) {
		parts := types.NewEmailFromParts(notification.Data.Local, notification.Data.Domain)
		if _, exists := hitList.Has(parts); exists {
			logger.WithFields(logrus.Fields{
				"notification": notification,
			}).Debug("Ignoring notification, as it's already known")
			return
		}

		vr := validator.Result{
			Validations: notification.Data.Validations,
			Steps:       notification.Data.Steps,
		}

		err := hitList.Add(parts, vr)
		if err != nil {
			logger.WithFields(logrus.Fields{
				"error": err,
				"data":  notification.Data,
				"ctx":   ctx.Err(),
			}).Error("Unable to add to hitlist")
		}

		if vr.Validations.IsValidationsForValidDomain() && !myFinder.Exact(parts.Domain) {
			myFinder.Refresh(hitList.GetValidAndUsageSortedDomains())
		}
	}
}
