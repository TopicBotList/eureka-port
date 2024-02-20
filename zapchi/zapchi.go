// Modified version of zapchi for Popplio
package zapchi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/topicbotlist/eureka-port/crypto"
	"go.uber.org/zap"
)

// Logger is a Chi middleware that logs each request recived using
// the provided unsugared logger
// Provide a name if you want to set the caller (`.Named()`)
// otherwise leave blank.
func Logger(l interface{}, name string) func(next http.Handler) http.Handler {
	var logger *zap.Logger

	switch l := l.(type) {
	case *zap.Logger:
		logger = l
	case *zap.SugaredLogger:
		logger = l.Desugar()
	default:
		panic("Unknown logger passed in. Please provide *Zap.SugaredLogger or *Zap.Logger")
	}

	logger = logger.Named(name)

	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			reqId := crypto.RandString(12)
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			t1 := time.Now()
			next.ServeHTTP(ww, r)

			logger.With(
				zap.Int("status", ww.Status()),
				zap.String("statusText", http.StatusText(ww.Status())),
				zap.String("method", r.Method),
				zap.String("url", r.URL.String()),
				zap.String("reqIp", r.RemoteAddr),
				zap.String("protocol", r.Proto),
				zap.Int("size", ww.BytesWritten()),
				zap.String("latency", time.Since(t1).String()),
				zap.String("userAgent", r.UserAgent()),
				zap.String("reqId", reqId),
			).Info("Got Request")
		}
		return http.HandlerFunc(fn)
	}
}
