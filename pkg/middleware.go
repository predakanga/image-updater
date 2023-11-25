package pkg

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"net"
	"net/http"
	"strings"
)

func ParseCIDRs(inputs []string) []*net.IPNet {
	toRet := make([]*net.IPNet, 0, len(inputs))
	for i, input := range inputs {
		// Convert any bare IPs to CIDR
		if !strings.ContainsRune(input, '/') {
			// IPv6 gets a /128 mask
			if strings.ContainsRune(input, ':') {
				input = input + "/128"
			} else {
				input = input + "/32"
			}
		}
		if _, cidr, err := net.ParseCIDR(input); err != nil {
			log.WithError(err).WithField("ip", inputs[i]).Fatal("Invalid IP address")
		} else {
			toRet = append(toRet, cidr)
		}
	}

	return toRet
}

func IPAllowlistHandler(handler http.Handler, allowed []*net.IPNet) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ipStr, _, err := net.SplitHostPort(r.RemoteAddr); err != nil {
			log.WithError(err).WithField("address", r.RemoteAddr).Warn("Could not decode remote address")
			w.WriteHeader(http.StatusForbidden)
			return
		} else {
			ip := net.ParseIP(ipStr)
			matched := false
			for _, network := range allowed {
				if network.Contains(ip) {
					matched = true
					break
				}
			}
			if !matched {
				w.WriteHeader(http.StatusForbidden)
				return
			}
		}
		handler.ServeHTTP(w, r)
	})
}

func SecretKeyHandler(handler http.Handler, name string, key string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(name) != key {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		handler.ServeHTTP(w, r)
	})
}

var (
	hookCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "image_updater",
		Subsystem: "http",
		Name:      "hooks_received",
		Help:      "The number of webhook calls received",
	}, []string{"code"})
	hooksRunning = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "image_updater",
		Subsystem: "http",
		Name:      "hooks_inflight",
		Help:      "The number of webhook calls currently being processed",
	})
	hookDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "image_updater",
		Subsystem: "http",
		Name:      "hooks_duration",
		Help:      "The response times of webhook calls",
	}, []string{})
)

func InstrumentHandler(handler http.Handler) http.Handler {
	// Track count and response codes
	handler = promhttp.InstrumentHandlerCounter(hookCount, handler)
	// In-flight hooks
	handler = promhttp.InstrumentHandlerInFlight(hooksRunning, handler)
	// And response times
	handler = promhttp.InstrumentHandlerDuration(hookDuration, handler)

	return handler
}
