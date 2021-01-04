package main

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

const (
	// the number of successful healthcheck responses required,
	// per scheme, for an ip/host check to pass
	healthcheckSuccess = 3
)

var (
	errHealthcheckRequest  = fmt.Errorf("error performing one or more healthchecks")
	errHealthcheckResponse = fmt.Errorf("error handling one or more healthchecks")
	errHealthcheckStatus   = fmt.Errorf("invalid status code returned from one or more healthchecks")
	errHealthcheckCritical = fmt.Errorf("critical error, all healthchecks failed")
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
func ensureHostHealthchecks(ip net.IP, host string) error {
	// we MUST perform healthchecks on both http and https protocols
	schemes := []string{"http", "https"}

	// success counter should be incremented after each successful healthcheck
	success := 0

	// attempt to validate the host url, any errors should be treated as fatal
	u, err := url.Parse(ip.String())
	if err != nil {
		return fmt.Errorf("error parsing host url: %w", err)
	}

	// failError represents the newest error received after all healthchecks
	// have completed
	var failError error

	for _, scheme := range schemes {
		// for each scheme, we want to perform healthcheckSuccess/N requests
		// to ensure consistency
		for i := 0; i < healthcheckSuccess; i++ {
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
				return fmt.Errorf("error building http request: %v", err)
			}

			// as we are using the server ip in the http request, we need to set
			// the host manually
			req.Host = host

			// attempt to perform http request
			res, err := httpClient.Do(req)
			if err != nil {
				failError = errHealthcheckRequest
				// TODO: zerolog
				//logCtx.WithError(err).Error("error performing http request")
				continue
			}

			// we don't read the body so an error shouldn't be classed as a failed
			// healthcheck
			if err := res.Body.Close(); err != nil {
				failError = errHealthcheckResponse
				// TODO: zerolog
				//logCtx.WithError(err).Error("error closing request body")
			}

			// successful http requests will only return 200 OK
			if res.StatusCode != http.StatusOK {
				failError = errHealthcheckStatus
				// TODO: zerolog
				//logCtx.Errorf("invalid http response code: %d", res.StatusCode)
				continue
			}

			success++
		}
	}

	// check success rate == required count
	passRate := (healthcheckSuccess * len(schemes))
	if success != passRate {
		return fmt.Errorf("failed %d out of %d healthchecks: %w", (passRate - success), passRate, failError)
	}

	return nil
}
