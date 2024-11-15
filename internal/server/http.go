package server

import (
	v1 "cobo-ucw-backend/api/ucw/v1"
	"cobo-ucw-backend/internal/conf"
	"cobo-ucw-backend/internal/middleware/auth"
	"cobo-ucw-backend/internal/service"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware/logging"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/middleware/tracing"
	"github.com/go-kratos/kratos/v2/middleware/validate"
	"github.com/go-kratos/kratos/v2/transport/http"
	"go.opentelemetry.io/otel/sdk/trace"
)

// NewHTTPServer new an HTTP server.
func NewHTTPServer(c *conf.Server, am *auth.JwtMiddleware, ucw *service.UserControlWalletService, logger log.Logger) *http.Server {
	var opts = []http.ServerOption{
		http.Middleware(
			recovery.Recovery(),
			tracing.Server(tracing.WithTracerProvider(trace.NewTracerProvider())),
			logging.Server(logger),
			am.Server(),
			validate.Validator(),
		),
		http.PathPrefix("/v1"),
	}
	if c.Http.Network != "" {
		opts = append(opts, http.Network(c.Http.Network))
	}
	if c.Http.Addr != "" {
		opts = append(opts, http.Address(c.Http.Addr))
	}
	if c.Http.Timeout != nil {
		opts = append(opts, http.Timeout(c.Http.Timeout.AsDuration()))
	}
	srv := http.NewServer(opts...)
	v1.RegisterUserControlWalletHTTPServer(srv, ucw)
	return srv
}
