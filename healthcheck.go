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
)

const (
	// the number of successful health check responses required,
	// per scheme, for an ip/host check to pass
	healthCheckSuccess = 3
)

var (
	errHealthCheckRequest  = fmt.Errorf("error performing one or more health checks")
	errHealthCheckResponse = fmt.Errorf("error handling one or more health checks")
	errHealthCheckStatus   = fmt.Errorf("invalid status code returned from one or more health checks")
	errHealthCheckCritical = fmt.Errorf("critical error, all health checks failed")
)

// Mockable http client interface
type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// Default http client with a 10 second timeout.
// We need to skip TLS verification as we perform queries on
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
	success := uint32(0)

	// attempt to validate the host url, any errors should be treated as fatal
	u, err := url.Parse(ip.String())
	if err != nil {
		return fmt.Errorf("error parsing host url: %w", err)
	}

	var wg sync.WaitGroup

	for _, scheme := range schemes {
		for i := 0; i < healthCheckSuccess; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				u.Scheme = scheme

				// TODO: zerolog
				//logCtx := log.WithFields(log.Fields{
				//	"url":  u,
				//	"host": host,
				//	"ip":   ip,
				//})

				// attempt to create http request, any errors should be treated as fatal
				// as the arguments will not change on the next iteration
				req, err := http.NewRequest(http.MethodGet, u.String(), nil)
				if err != nil {
					// TODO: zerolog
					// fmt.Errorf("error building http request: %v", err)
				}

				// as we are using the server ip in the http request, we need to set
				// the host manually
				req.Host = host

				// attempt to perform http request
				res, err := httpClient.Do(req)
				if err != nil {
					//failError = errHealthCheckRequest
					// TODO: zerolog
					//logCtx.WithError(err).Error("error performing http request")
					return
				}

				// we don't read the body so an error shouldn't be classed as a failed health check
				if err := res.Body.Close(); err != nil {
					//failError = errHealthCheckResponse
					// TODO: zerolog
					//logCtx.WithError(err).Error("error closing request body")
				}

				// successful http requests will only return 200 OK
				if res.StatusCode != http.StatusOK {
					//failError = errHealthCheckStatus
					// TODO: zerolog
					//logCtx.Errorf("invalid http response code: %d", res.StatusCode)
					return
				}

				atomic.AddUint32(&success, 1)
			}()
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
