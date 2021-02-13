package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"strconv"
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

	// comma separated list of Route53 records to be updated, e.g: syscll.org,ingress.syscll.org,haproxy.syscll.org
	envAWSRoute53Records = "AWS_ROUTE53_RECORDS"

	// poll interval for route53 updates, default: 30s
	envPollInterval = "POLL_INTERVAL"

	// port to bind local http server to, default: 8081
	envPort = "PORT"
)

func main() {
	// UNIX Time is faster and smaller than most timestamps
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	// parse valid aws ec2 tag into parts, tag[0] == key, tag[1] == value
	tag := strings.SplitN(os.Getenv(envAWSEC2Tag), ":", 2)
	if len(tag) != 2 {
		log.Fatal().Msgf("invalid aws ec2 tag: %s", envAWSEC2Tag)
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
	interval, err := time.ParseDuration(p)
	if err != nil {
		log.Fatal().Msgf("invalid poll interval: %s: %v", interval, err)
	}

	// parse port
	port := 8081
	if p := os.Getenv(envPort); p != "" {
		port, err = strconv.Atoi(p)
		if err != nil {
			log.Fatal().Msgf("invalid port: %s: %v", p, err)
		}
	}

	// configure a channel to listen for exit signals in order to perform
	// a graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	// start the local http server
	srv := startHTTP(port)

	// start a ticker at given intervals
	t := time.NewTicker(interval)
	log.Info().Msgf("service started, will attempt to assign ingress service ip addresses every %s", interval)

	for {
		select {
		case <-stop:
			log.Info().Msg("received stop signal, attempting graceful shutdown")

			// stop ticker
			t.Stop()

			// gracefully shutdown
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

			if err := srv.Shutdown(ctx); err != nil {
				cancel()
				log.Fatal().Err(err).Msg("error shuting down http server")
			}

			cancel()
			os.Exit(0)
		case <-t.C:
			poll(region, tag, records)
		}
	}
}

// poll periodically attempts to retrieve the public ip addrs of a set of ec2 instances
// and ensure the provided route53 record sets are configured
func poll(region string, tag []string, records []string) {
	// configure aws service manager
	aws := newAWSManager(region)

	// get all public ip addrs of ec2 instances with given tag
	ips, err := aws.getTaggedEC2PublicIPAddrs(tag[0], tag[1])
	if err != nil {
		log.Error().Err(err).Msg("error getting public ip addrs")
		return
	}

	if len(ips) == 0 {
		log.Error().Msg("no ip addrs found, will not update")
		return
	}

	log.Info().Msgf("found %d ip addrs", len(ips))

	// reset the health check gauge before attempting to perform
	// current health checks
	healthCheckFailures.Set(0)

	var wg sync.WaitGroup

	// attempt to upsert record set with given ip addrs
	for _, record := range records {
		wg.Add(1)
		go func(record string) {
			defer wg.Done()

			var healthy []net.IP

			// for each ip addr, perform health checks to ensure the ip addr successfully
			// handles a request to the host record
			for _, ip := range ips {
				if err := ensureHostHealthChecks(httpClient, ip, record); err != nil {
					log.Error().Err(err).IPAddr("ip", ip).Str("record", record).Msg("failed all health checks, will not add this record")
					continue
				}
				healthy = append(healthy, ip)
			}

			if len(healthy) == 0 {
				log.Error().Str("record", record).Msg("all health checks failed, will not update")
				return
			}

			if err := aws.ensureRoute53RecordSet(record, healthy); err != nil {
				log.Error().Err(err).Str("record", record).Msg("error performing change on resource record")
				return
			}

			log.Info().Str("record", record).Int("ip_addrs", len(healthy)).Msg("successfully updated record with healthy ip addrs")
		}(record)
	}

	wg.Wait()
	log.Info().Msg("all records are up to date")
}
