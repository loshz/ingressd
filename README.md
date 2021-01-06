# ingressd
[![Build Status](https://github.com/syscll/ingressd/workflows/build/badge.svg)](https://github.com/syscll/ingressd/actions) [![MIT licensed](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE) [![Quay.io](https://img.shields.io/badge/container-quay.io-red)](https://quay.io/repository/syscll/ingressd)

A lightweight daemon used to update Route53 records with the IP addresses of your ingress services, as well as perform health checks on desired hosts.

## Architecture
![ingressd architecture](https://github.com/syscll/ingressd/blob/main/ingressd.png?raw=true)

0. Configure `ingressd` with list of Route53 host records.
1. Query EC2 for nodes with a specific tag, and return their public IP addresses.
2. Make several health checks against each ingress service IP address with specific host header (`curl -H "Host: example.com" http://192.168.0.1`).
3. Update Route53 records with IP addresses that have passed all health checks.

## Usage
As `ingressd` is currently configured to use AWS [Instance Roles](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/iam-roles-for-amazon-ec2.html), the host will need to have a role with at least `AmazonEC2ReadOnlyAccess` and a Route53 policy with the following actions:`ChangeResourceRecordSets`, `ListResourceRecordSets`, `ListHostedZones`.

### Config
The service can be configured by setting the following environment variables:

| Name | Type | Description |
| ---- | ---- | ----------- |
| `AWS_EC2_TAG` | string | key:value of EC2 tag to query for instances |
| `AWS_REGION` | string | AWS region of EC2 instances to query |
| `AWS_ROUTE53_RECORDS` | string slice | Comma separated list of Route53 records to be updated |
| `POLL_INTERVAL` | string | Poll interval for Route53 updates |

### Kubernetes
A simple single container Pod spec:
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: ingressd
  labels:
    app.kubernetes.io/name: ingressd
spec:
  securityContext:
    runAsUser: 2000
    runAsGroup: 2000
    fsGroup: 2000
  containers:
  - name: ingressd
    image: quay.io/syscll/ingressd:v0.1.0
    command:
    - ingressd
    livenessProbe:
      httpGet:
        path: /healthz
        port: 8081
      initialDelaySeconds: 5
      periodSeconds: 3
    ports:
    - containerPort: 8081
    env:
    - name: AWS_EC2_TAG
      value: "Name:haproxy"
    - name: AWS_REGION
      value: "eu-west-1"
    - name: AWS_ROUTE53_RECORDS
      value: "syscll.org,ingress.syscll.org,haproxy.syscll.org"
    - name: POLL_INTERVAL
      value: "10s"
```

## TODO
- Expose Prometheus metrics
- Allow different types of health checks (tcp, etc.)
- Allow rate limiting
- Allow health check success configuration
