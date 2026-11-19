package rest

import (
	"net/http"
	"runtime/debug"
	"time"

	"go.uber.org/zap"

	"github.com/StreamNext/Backend/internal/apperr"
)

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic", zap.Any("panic", rec), zap.ByteString("stack", debug.Stack()))
				s.writeError(w, apperr.New(apperr.Internal, "internal error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Info("request", zap.String("method", r.Method), zap.String("path", r.URL.Path),
			zap.Int("status", rec.status), zap.Duration("took", time.Since(start)))
	})
}
