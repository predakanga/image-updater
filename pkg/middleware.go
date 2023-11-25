package pkg

import (
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
