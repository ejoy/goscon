package main

import (
	"errors"
	"strconv"
	"time"

	"github.com/ejoy/goscon/scp"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	connectionAccepts = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "goscon_connection_accepts",
		Help: "times of accept connection from client",
	})

	connectionAcceptFails = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "goscon_connection_accept_fails",
		Help: "failed times of accept connection from client",
	})

	connectionCloses = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "goscon_connection_close",
		Help: "times of close connection from client",
	})

	handshakeErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "goscon_handshake_errors",
		Help: "number of handshake errors, partitioned by error code",
	}, []string{"code"})

	connectionReuses = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "goscon_connection_reuses",
		Help: "times of reuse successfully",
	})

	connectionResend = prometheus.NewSummary(prometheus.SummaryOpts{
		Name:       "goscon_connection_resend",
		Help:       "bytes of data resend while reuse",
		Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		MaxAge:     time.Hour * 12,
		AgeBuckets: 12,
	})

	connectionReuseFails = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "goscon_connection_reuse_fails",
		Help: "times of reuse failed",
	})

	upstreamErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "goscon_upstream_fails",
		Help: "times of failed to connect to upstream",
	})
)

func init() {
	prometheus.MustRegister(connectionAccepts)
	prometheus.MustRegister(connectionAcceptFails)
	prometheus.MustRegister(connectionCloses)
	prometheus.MustRegister(handshakeErrors)
	prometheus.MustRegister(connectionReuses)
	prometheus.MustRegister(connectionResend)
	prometheus.MustRegister(connectionReuseFails)
	prometheus.MustRegister(upstreamErrors)
}

func metricOnHandshakeError(err error) {
	var serr *scp.Error
	if errors.As(err, &serr) {
		handshakeErrors.With(prometheus.Labels{"code": strconv.Itoa(serr.Code)}).Inc()
	} else {
		handshakeErrors.With(prometheus.Labels{"code": strconv.Itoa(scp.SCPStatusNetworkError)}).Inc()
	}
}
