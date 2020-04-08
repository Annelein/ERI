package handlers

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	RequestID contextValue = "request_id"
)

type contextValue string

func (cv contextValue) String() string {
	return string(cv)
}

func WithRequestLogger(logger logrus.FieldLogger) HandlerWrapper {

	logger = logger.WithField("middleware", "request_logger")
	return func(handler http.Handler) http.Handler {

		var reqID uint64
		m := sync.Mutex{}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var now time.Time

			writer := NewCustomResponseWriter(w)

			m.Lock()
			reqID++
			rid := strconv.FormatUint(reqID, 10)
			m.Unlock()

			logger := logger.WithFields(logrus.Fields{
				"request_id": rid,
				"method":     r.Method,
				"uri":        r.RequestURI,
			})

			r = r.WithContext(context.WithValue(r.Context(), RequestID, rid))

			logger.Debug("Request start")

			defer func(w *CustomResponseWriter) {

				logger.WithFields(logrus.Fields{
					"time_µs":             time.Since(now).Microseconds(),
					"response_size_bytes": w.BytesWritten,
					"http_status":         w.Status,
				}).Debug("Request end")
			}(writer)

			now = time.Now()
			handler.ServeHTTP(writer, r)
		})
	}
}
