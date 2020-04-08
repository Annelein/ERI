package handlers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

type TakeMaxDuration interface {
	TakeMaxDuration(count int64, maxWait time.Duration) (time.Duration, bool)
}

func NewRateLimitHandler(logger logrus.FieldLogger, b TakeMaxDuration, maxDelay time.Duration) HandlerWrapper {
	if b == nil {
		logger.Info("Rate Limiter disabled, no bucket defined.")
		return func(h http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.ServeHTTP(w, r)
			})
		}
	}

	logger = logger.WithField("middleware", "rate_limiter")
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			d, ok := b.TakeMaxDuration(1, maxDelay)

			logger := logger.WithFields(logrus.Fields{
				"remote_addr":      r.RemoteAddr,
				RequestID.String(): r.Context().Value(RequestID),
				"max_delay":        maxDelay.String(),
				"delay":            d.String(),
			})

			if !ok {
				logger.Warn("rate limit: aborting request, above max allowed delay")

				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = fmt.Fprint(w, "Server busy, request aborted")
				return
			}

			if d > 0 {
				logger.Warn("rate limit: throttling request, will continue after delay")
				time.Sleep(d)
			}

			h.ServeHTTP(w, r)
		})
	}
}
