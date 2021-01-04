package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	// key/value of EC2 tag to query for instances, e.g: Name:haproxy
	envAWSEC2Tag = "AWS_EC2_TAG"

	// AWS region of EC2 instances to query
	envAWSRegion = "AWS_REGION"

	// list of Route53 records to be updated
	envAWSRoute53Records = "AWS_ROUTE53_RECORDS"

	// poll interval for route53 updates, default: 30s
	envPollInterval = "POLL_INTERVAL"
)

var tlsSkipVerify bool

func main() {
	// UNIX Time is faster and smaller than most timestamps
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	// parse valid aws ec2 tag
	tag := os.Getenv(envAWSEC2Tag)
	if tag == "" {
		log.Fatal().Msgf("missing aws ec2 tag: %s", envAWSEC2Tag)
	}

	// parse aws region
	region := os.Getenv(envAWSRegion)
	if region == "" {
		log.Fatal().Msgf("missing aws region: %s", envAWSRegion)
	}

	// parse route53 records
	records := strings.Split(os.Getenv(envAWSRoute53Records), ",")
	if len(records) == 0 {
		log.Fatal().Msgf("missing aws route53 records: %s", envAWSRoute53Records)
	}

	// parse poll interval
	p := os.Getenv(envPollInterval)
	if p == "" {
		p = "30s"
	}
	poll, err := time.ParseDuration(p)
	if err != nil {
		log.Fatal().Msgf("invalid poll interval: %s: %v", poll, err)
	}

	// configure a channel to listen for exit signals in order to perform
	// a graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)

	// configure and run web server for health check,
	// we don't care about any errors as the healthcheck caller
	// should interpret this as fatal
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	})
	go http.ListenAndServe(":8081", nil)
	log.Info().Msg("health check registered on localhost:8081/healthz")

	// start a ticker at `poll` intervals
	t := time.NewTicker(poll)
	log.Info().Msgf("service started, will attempt assign ip addresses every %s", poll)

	for {
		select {
		case <-stop:
			log.Info().Msg("received stop signal, attempting graceful shutdown")

			// stop ticker
			t.Stop()

			// TODO: graceful shutdown

			os.Exit(0)
		case <-t.C:
			// TODO: poll
		}
	}
}

// poll periodically attempts to retrieve the public ip addrs of a set of ec2 instances
// and ensure the provided route53 record sets are configured
func poll(region, tag string, records []string) error {
	// configure aws service manager
	aws := newAWSManager(region)

	// get all public ip addrs of ec2 instances with given tag
	ips, err := aws.getTaggedEC2PublicIPAddrs(tag)
	if err != nil {
		return fmt.Errorf("error getting public ip addrs: %v", err)
	}

	if len(ips) == 0 {
		return fmt.Errorf("no ip addrs found, will try again")
	}

	log.Printf("found %d ip addrs", len(ips))

	var wg sync.WaitGroup

	// attempt to upsert record set with given ip addrs
	for _, record := range records {
		wg.Add(1)
		go func(record string) {
			defer wg.Done()

			var healthy []net.IP

			// for each ip addr, perform health check to ensure the node successfully
			// handles a request to the host record
			for _, ip := range ips {
				if err := ensureHostHealthChecks(httpClient, ip, record); err != nil {
					// TODO: zerolog
					continue
				}
				healthy = append(healthy, ip)
			}

			if err := aws.ensureRoute53RecordSet(record, healthy); err != nil {
				// TODO: zerolog
				//log.WithError(err).WithField("host", host).Error("error performing change on resource record")
			}
		}(record)
	}

	wg.Wait()

	//log.Printf("all records are up to date")

	return nil
}
