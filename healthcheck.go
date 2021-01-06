package main

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	// the number of successful health check responses required,
	// per scheme, for an ip/host check to pass
	healthCheckSuccess = 3
)

// Mockable http client interface
type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Default http client with a 10 second timeout.
// TLS verification must be skipped as we perform queries on
// ip addrs, not urls.
var httpClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
}

// ensureHostHealthChecks performs multiple http/s health checks on a given ip/host.
// the number of successful attempts MUST match the required amount in order for
// this method to return err == nil
func ensureHostHealthChecks(httpClient httpDoer, ip net.IP, host string) error {
	// we MUST perform health checks on both http and https protocols
	schemes := []string{"http", "https"}

	// success counter should be incremented after each successful health check
	var success uint32

	// attempt to validate the host url, any errors should be treated as fatal
	u, err := url.Parse(ip.String())
	if err != nil {
		return fmt.Errorf("error parsing host url: %w", err)
	}

	var wg sync.WaitGroup

	for _, scheme := range schemes {
		for i := 0; i < healthCheckSuccess; i++ {
			wg.Add(1)
			go func(scheme string) {
				defer wg.Done()

				logCtx := map[string]interface{}{
					"url":  u.String(),
					"host": host,
					"ip":   ip,
				}

				// attempt to create http request, any errors should be treated as fatal
				// as the arguments will not change on the next iteration
				req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s://%s", scheme, u), nil)
				if err != nil {
					log.Error().Err(err).Fields(logCtx).Msg("error building http request")
					return
				}

				// as we are using the server ip in the http request, we need to set
				// the host manually
				req.Host = host

				// attempt to perform http request
				res, err := httpClient.Do(req)
				if err != nil {
					log.Error().Err(err).Fields(logCtx).Msg("error performing http request")
					return
				}

				// we don't read the body so an error shouldn't be classed as a failed health check
				defer res.Body.Close()

				// successful http requests will only return 200 OK
				if res.StatusCode != http.StatusOK {
					log.Error().Fields(logCtx).Msgf("invalid http response code: %d", res.StatusCode)
					return
				}

				atomic.AddUint32(&success, 1)
			}(scheme)
		}
	}

	wg.Wait()

	// check success rate == required count
	passRate := (healthCheckSuccess * len(schemes))
	if int(success) != passRate {
		return fmt.Errorf("failed %d out of %d health checks", (passRate - int(success)), passRate)
	}

	return nil
}
