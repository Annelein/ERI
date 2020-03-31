package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/pprof"
	"os"

	"github.com/Dynom/ERI/validator"

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
			TimestampFormat: `15:04:05.999999`, //time.RFC3339Nano,
		})
	}

	logger.SetOutput(os.Stdout)
	level, err := logrus.ParseLevel(conf.Server.Log.Level)
	if err == nil {
		logger.SetLevel(level)
	}

	return logger, logger.WriterLevel(level), err
}

func configureProfiler(mux *http.ServeMux, conf config.Config) {
	var prefix string
	if conf.Server.Profiler.Prefix != "" {
		prefix = conf.Server.Profiler.Prefix
	} else {
		prefix = "debug"
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
