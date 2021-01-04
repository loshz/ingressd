package main

import (
	"fmt"
	"net"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/route53"
)

type mockRoute53ReadWriter struct {
	changeFunc func(*route53.ChangeResourceRecordSetsInput) (*route53.ChangeResourceRecordSetsOutput, error)
	listFunc   func(*route53.ListHostedZonesInput) (*route53.ListHostedZonesOutput, error)
	err        error
}

func (m mockRoute53ReadWriter) ChangeResourceRecordSets(input *route53.ChangeResourceRecordSetsInput) (*route53.ChangeResourceRecordSetsOutput, error) {
	return m.changeFunc(input)
}

func (m mockRoute53ReadWriter) ListHostedZones(input *route53.ListHostedZonesInput) (*route53.ListHostedZonesOutput, error) {
	return m.listFunc(input)
}

func TestGetRoute53HostedZoneID(t *testing.T) {
	t.Parallel()

	testTable := make(map[string]mockRoute53ReadWriter)

	testTable["TestListHostedZonesError"] = mockRoute53ReadWriter{
		listFunc: func(*route53.ListHostedZonesInput) (*route53.ListHostedZonesOutput, error) {
			return nil, fmt.Errorf("aws error")
		},
		err: fmt.Errorf("error listing hosted zones: aws error"),
	}

	testTable["TestNoHostedZonesError"] = mockRoute53ReadWriter{
		listFunc: func(*route53.ListHostedZonesInput) (*route53.ListHostedZonesOutput, error) {
			return &route53.ListHostedZonesOutput{
				HostedZones: []*route53.HostedZone{
					{
						Name: aws.String("syscll.org"),
					},
				},
			}, nil
		},
		err: fmt.Errorf("no zone id found for: syscll.org"),
	}

	testTable["TestSuccess"] = mockRoute53ReadWriter{
		listFunc: func(*route53.ListHostedZonesInput) (*route53.ListHostedZonesOutput, error) {
			return &route53.ListHostedZonesOutput{
				HostedZones: []*route53.HostedZone{
					{
						Id:   aws.String("zone-1"),
						Name: aws.String("syscll.org."),
					},
					{
						Id:   aws.String("zone-2"),
						Name: aws.String("ingressd.syscll.org."),
					},
				},
			}, nil
		},
		err: nil,
	}

	for name, test := range testTable {
		t.Run(name, func(t *testing.T) {
			mgr := awsManager{
				route53: test,
			}

			id, err := mgr.getRoute53HostedZoneID("syscll.org")
			if test.err != nil && err.Error() != test.err.Error() {
				t.Errorf("expected error: '%v', got: '%v'", test.err, err)
			}
			if test.err == nil {
				if err != nil {
					t.Errorf("expected error: nil, got: %v", err)
				}
				if id != "zone-1" {
					t.Errorf("expected zone id: 'zone-1', got: '%s'", id)
				}
			}
		})
	}
}

type mockEC2Describer struct {
	describeFunc func(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error)
	err          error
}

func (m mockEC2Describer) DescribeInstances(input *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
	return m.describeFunc(input)
}

func TestGetEC2PublicIPAddrs(t *testing.T) {
	t.Parallel()

	testTable := make(map[string]mockEC2Describer)

	testTable["TestListHostedZonesError"] = mockEC2Describer{
		describeFunc: func(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
			return nil, fmt.Errorf("aws error")
		},
		err: fmt.Errorf("error describing instances: aws error"),
	}

	testTable["TestSuccess"] = mockEC2Describer{
		describeFunc: func(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
			return &ec2.DescribeInstancesOutput{
				Reservations: []*ec2.Reservation{
					{
						Instances: []*ec2.Instance{
							{
								InstanceId:      aws.String("1"),
								PublicIpAddress: aws.String("192.168.0.1"),
								State: &ec2.InstanceState{
									Name: aws.String(ec2.InstanceStateNameRunning),
								},
							},
							{
								InstanceId:      aws.String("2"),
								PublicIpAddress: aws.String("192.168.0.2"),
								State: &ec2.InstanceState{
									Name: aws.String(ec2.InstanceStateNameRunning),
								},
							},
							{
								InstanceId:      aws.String("3"),
								PublicIpAddress: aws.String("192.168.0.3"),
								State: &ec2.InstanceState{
									Name: aws.String(ec2.InstanceStateNameTerminated),
								},
							},
							{
								InstanceId:      aws.String("4"),
								PublicIpAddress: aws.String("192.168.0.4"),
								State: &ec2.InstanceState{
									Name: aws.String(ec2.InstanceStateNameStopping),
								},
							},
						},
					},
				},
			}, nil
		},
		err: nil,
	}

	for name, test := range testTable {
		t.Run(name, func(t *testing.T) {
			mgr := awsManager{
				ec2: test,
			}

			// TODO: use tag
			ips, err := mgr.getTaggedEC2PublicIPAddrs("")
			if test.err != nil && err.Error() != test.err.Error() {
				t.Errorf("expected error: '%v', got: '%v'", test.err, err)
			}
			if test.err == nil {
				if err != nil {
					t.Errorf("expected error: nil, got: %v", err)
				}

				for _, ip := range ips {
					if ip.String() != "192.168.0.1" && ip.String() != "192.168.0.2" {
						t.Fatalf("incorrect list of ip addrs: %s", ips)
					}
				}
			}
		})
	}
}

func TestEnsureRoute53RecordSet(t *testing.T) {
	t.Parallel()

	testTable := make(map[string]mockRoute53ReadWriter)

	testTable["TestHostedZoneIDError"] = mockRoute53ReadWriter{
		listFunc: func(*route53.ListHostedZonesInput) (*route53.ListHostedZonesOutput, error) {
			return nil, fmt.Errorf("route53 error")
		},
		err: fmt.Errorf("error getting route53 hosted zone: error listing hosted zones: route53 error"),
	}

	testTable["TestChangeRecordSetError"] = mockRoute53ReadWriter{
		changeFunc: func(*route53.ChangeResourceRecordSetsInput) (*route53.ChangeResourceRecordSetsOutput, error) {
			return nil, fmt.Errorf("route53 error")
		},
		listFunc: func(*route53.ListHostedZonesInput) (*route53.ListHostedZonesOutput, error) {
			return &route53.ListHostedZonesOutput{
				HostedZones: []*route53.HostedZone{
					{
						Id:   aws.String("zone-1"),
						Name: aws.String("syscll.org."),
					},
				},
			}, nil
		},
		err: fmt.Errorf("error performing change to record set: route53 error"),
	}

	testTable["TestChangeRecordSetError"] = mockRoute53ReadWriter{
		changeFunc: func(*route53.ChangeResourceRecordSetsInput) (*route53.ChangeResourceRecordSetsOutput, error) {
			return nil, nil
		},
		listFunc: func(*route53.ListHostedZonesInput) (*route53.ListHostedZonesOutput, error) {
			return &route53.ListHostedZonesOutput{
				HostedZones: []*route53.HostedZone{
					{
						Id:   aws.String("zone-1"),
						Name: aws.String("syscll.org"),
					},
				},
			}, nil
		},
		err: nil,
	}

	for name, test := range testTable {
		t.Run(name, func(t *testing.T) {
			mgr := awsManager{
				route53: test,
			}

			var ips []net.IP
			ips = append(ips, net.ParseIP("192.168.0.1"))

			err := mgr.ensureRoute53RecordSet("syscll.org", ips)
			if test.err != nil && err.Error() != test.err.Error() {
				t.Errorf("expected error: '%v', got: '%v'", test.err, err)
			}
			if test.err == nil {
				if err != nil {
					t.Errorf("expected error: nil, got: %v", err)
				}
			}
		})
	}
}
