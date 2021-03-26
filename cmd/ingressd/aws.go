package main

import (
	"fmt"
	"net"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/rs/zerolog/log"
)

// ec2Describer implements functions for describing ec2 instance data
type ec2Describer interface {
	DescribeInstances(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error)
}

// route53ReadWriter implements functions for reading and writing to route53
type route53ReadWriter interface {
	ChangeResourceRecordSets(*route53.ChangeResourceRecordSetsInput) (*route53.ChangeResourceRecordSetsOutput, error)
	ListHostedZones(*route53.ListHostedZonesInput) (*route53.ListHostedZonesOutput, error)
}

// service manager for aws ec2 and route53
type awsManager struct {
	// aws region of the below services
	region string

	// aws service for interacting with the ec2 api
	ec2 ec2Describer

	// aws service for interacting with the route53 api
	route53 route53ReadWriter
}

// create new aws services with a reusable configured session
func newAWSManager(region string) awsManager {
	sess := session.Must(session.NewSession(&aws.Config{
		Region: aws.String(region),
	}))

	return awsManager{
		region:  region,
		ec2:     ec2.New(sess),
		route53: route53.New(sess),
	}
}

// getTaggedEC2PublicIPAddrs queries ec2 for all instances of a given name,
// returning their public ip addr if configured
func (mgr awsManager) getTaggedEC2PublicIPAddrs(key, value string) ([]net.IP, error) {
	input := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String(fmt.Sprintf("tag:%s", key)),
				Values: []*string{
					aws.String(value),
				},
			},
		},
	}

	res, err := mgr.ec2.DescribeInstances(input)
	if err != nil {
		return nil, fmt.Errorf("error describing instances: %w", err)
	}

	var ips []net.IP
	for _, reservation := range res.Reservations {
		for _, instance := range reservation.Instances {
			// check instance is running
			if aws.StringValue(instance.State.Name) != ec2.InstanceStateNameRunning {
				log.Info().Str("instance.id", aws.StringValue(instance.InstanceId)).Msg("skipping instance as state != running")
				continue
			}

			// check public ip addr is valid
			if publicIP := net.ParseIP(aws.StringValue(instance.PublicIpAddress)); publicIP != nil {
				ips = append(ips, publicIP)
			}
		}
	}

	return ips, nil
}

// getRoute53HostedZoneID attempts to match a given host addr to a Route53 Hosted Zone.
// If a match is found, the zone id is returned
func (mgr awsManager) getRoute53HostedZoneID(host string) (string, error) {
	zones, err := mgr.route53.ListHostedZones(&route53.ListHostedZonesInput{})
	if err != nil {
		return "", fmt.Errorf("error listing hosted zones: %w", err)
	}

	var found route53.HostedZone

	for _, zone := range zones.HostedZones {
		// aws will return will return the fully qualified dns record,
		// so we need to strip the last '.'
		name := strings.TrimSuffix(aws.StringValue(zone.Name), ".")

		// if the host addr has the suffix of zone name, we have a potential match.
		// however, we should also check the length of the zone in case of duplicate matches,
		// for example: a host with suffix 'ingressd.syscll.org' would match both 'ingressd.syscll.org'
		// and 'syscll.org'.
		// in this case, we should prefer the most precise match: 'syscll.org'
		if strings.HasSuffix(host, name) && len(name) > len(aws.StringValue(found.Name)) {
			found = *zone
		}
	}

	id := aws.StringValue(found.Id)
	if id == "" {
		return "", fmt.Errorf("no zone id found for: %s", host)
	}

	return id, nil
}

// ensureRoute53RecordSet attempts to upsert a Route53 A record for a given
// host and set of ip addrs
func (mgr awsManager) ensureRoute53RecordSet(host string, ips []net.IP) error {
	if len(ips) == 0 {
		return fmt.Errorf("no ips provided")
	}

	// loop through each of the given ip addrs and create a ResourceRecord for each
	var records []*route53.ResourceRecord
	for _, ip := range ips {
		records = append(records, &route53.ResourceRecord{
			Value: aws.String(ip.String()),
		})
	}

	// create change record of type A with a 60s TTL
	change := &route53.Change{
		Action: aws.String(route53.ChangeActionUpsert),
		ResourceRecordSet: &route53.ResourceRecordSet{
			Name:            aws.String(host),
			ResourceRecords: records,
			TTL:             aws.Int64(60),
			Type:            aws.String(route53.RRTypeA),
		},
	}

	// attempt to automatically get the hosted zone id for the given host
	zoneID, err := mgr.getRoute53HostedZoneID(host)
	if err != nil {
		return fmt.Errorf("error getting route53 hosted zone: %w", err)
	}

	input := &route53.ChangeResourceRecordSetsInput{
		ChangeBatch: &route53.ChangeBatch{
			Changes: []*route53.Change{change},
		},
		HostedZoneId: aws.String(zoneID),
	}

	if _, err := mgr.route53.ChangeResourceRecordSets(input); err != nil {
		return fmt.Errorf("error performing change to record set: %w", err)
	}

	return nil
}
