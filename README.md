# target-exporter

Go service that exports a metrics endpoint with the targets for each node.

## Setup

```bash
helm install target-exporter charts/target-exporter --namespace=target-exporter --create-namespace 
```

to uninstall:

```bash
helm uninstall -n target-exporter target-exporter
```

See also NOTES.txt

## Build

E.g.:

```bash
docker build -t target-exporter --platform=linux/amd64 .
docker login
docker tag target-exporter cristianohelio/target-exporter:0.0.5
docker push cristianohelio/target-exporter:0.0.5
```

then scale down & up the deployment to reapply the image (if tag didn't change, remember to set `pullPolicy: Always` in
the deployment)

```bash
kubectl scale deployment target-exporter --replicas=0 -n target-exporter 
kubectl scale deployment target-exporter --replicas=1 -n target-exporter 
```

## Required configuration

It is necessary to supply a `kubeconfig` to allow communication with the Kubernetes API. This can be done by simply 
placing a kubeconfig in the Helm chart directory (in `charts/target-exporter`) named as `ecoqube-dev.kubeconfig`. 
This file will be mounted in the container as a volume from a Secret created for this purpose.

## Testing

### Get request to get targets

```json
curl localhost:8080/api/v1/targets
```

Successful response:

```json
{"targets":{"scheduling-dev-wkld-md-0-4kb8j":100000,"scheduling-dev-wkld-md-0-9tnbl":30,"scheduling-dev-wkld-md-0-l4n2t":50}}%                                                                           
```

### Post request to set targets

```bash
curl -X POST localhost:8080/api/v1/targets \
-H 'Content-Type: application/json' \
-d '{"targets":{"scheduling-dev-wkld-md-0-4kb8j":100000,"scheduling-dev-wkld-md-0-9tnbl":30,"scheduling-dev-wkld-md-0-l4n2t":50}}'
```

Successful response:

```json
{"message":"success"}%
```

### Get actual CPU usage per each node in range 

- Results is in steps of 1 second, e.g. from 12:00:30 to 12:00:40 gives 10 measurements, last second not inclusive.
- Dates are expressed in RFC3339 ISO format.

```bash
curl -X GET 'localhost:8080/api/v1/actualCpuUsageByRangeSeconds?start=2022-12-09T12:01:24.429Z&end=2022-12-09T12:01:34.429Z'
```

<details open>
<summary>Successful response:</summary>
<br>
```json
[{
	"nodeName": "ecoqube-dev-default-worker-topo-lx7l2-65d7746-bg5rp",
	"usage": [{
		"timestamp": "2022-12-09T13:01:24+01:00",
		"data": 5.688636363636107
	}, {
		"timestamp": "2022-12-09T13:01:25+01:00",
		"data": 5.688636363636107
	}, {
		"timestamp": "2022-12-09T13:01:26+01:00",
		"data": 5.688636363636107
	}, {
		"timestamp": "2022-12-09T13:01:27+01:00",
		"data": 5.688636363636107
	}, {
		"timestamp": "2022-12-09T13:01:28+01:00",
		"data": 5.482575757575816
	}, {
		"timestamp": "2022-12-09T13:01:29+01:00",
		"data": 5.482575757575816
	}, {
		"timestamp": "2022-12-09T13:01:30+01:00",
		"data": 5.482575757575816
	}, {
		"timestamp": "2022-12-09T13:01:31+01:00",
		"data": 5.482575757575816
	}, {
		"timestamp": "2022-12-09T13:01:32+01:00",
		"data": 5.482575757575816
	}, {
		"timestamp": "2022-12-09T13:01:33+01:00",
		"data": 5.409090909091034
	}]
}, {
	"nodeName": "ecoqube-dev-default-worker-topo-lx7l2-65d7746-j7npf",
	"usage": [{
		"timestamp": "2022-12-09T13:01:24+01:00",
		"data": 5.743181818181924
	}, {
		"timestamp": "2022-12-09T13:01:25+01:00",
		"data": 5.743181818181924
	}, {
		"timestamp": "2022-12-09T13:01:26+01:00",
		"data": 5.743181818181924
	}, {
		"timestamp": "2022-12-09T13:01:27+01:00",
		"data": 5.41136363636339
	}, {
		"timestamp": "2022-12-09T13:01:28+01:00",
		"data": 5.41136363636339
	}, {
		"timestamp": "2022-12-09T13:01:29+01:00",
		"data": 5.41136363636339
	}, {
		"timestamp": "2022-12-09T13:01:30+01:00",
		"data": 5.41136363636339
	}, {
		"timestamp": "2022-12-09T13:01:31+01:00",
		"data": 5.41136363636339
	}, {
		"timestamp": "2022-12-09T13:01:32+01:00",
		"data": 5.421212121212108
	}, {
		"timestamp": "2022-12-09T13:01:33+01:00",
		"data": 5.421212121212108
	}]
}]
```
</details>

## Notes

- Port-forward with `kubectl port-forward -n kube-prom-stack prometheus-kube-prometheus-stack-prometheus-0 9090`
  and check under `Status > Targets` if the target was scraped successfully.

https://prometheus.io/docs/instrumenting/writing_exporters/#target-labels-not-static-scraped-labels

## TODOs

- [x] Prometheus doesn't have permission to scrape resources in namespaces different from its own (kube-prom-stack). It
  would be better to place target-exporter into its own namespace and give Prometheus permission to scrape it.
- [x] Create Helm chart
- [x] Create a metric like `cpu-diff` and create a timeseries per each node with test values.
- [x] Graceful shutdown
- [x] Get and set targets through a REST API
- [ ] Swap out plain Prometheus in TAS cluster for kube-prometheus-stack or just add the Prometheus Operator and deploy
  service. Would be cool to then propose it as a PR to the TAS team.
- [ ] Health and readiness checks
- [ ] Leveled logging
