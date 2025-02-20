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
docker tag target-exporter cristianohelio/target-exporter:0.0.8
docker push cristianohelio/target-exporter:0.0.8
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

### Post request to spawn workload

Note that the nodes must contain the relative workload type label, e.g. `ecoqube.eu/workload-type: storage`.

```bash
curl -X POST localhost:8080/api/v1/workloads \
-H 'Content-Type: application/json' \
-d '{"jobLength": , "cpuTarget": cpuTarget, "workloadType": "storage"}'
```
```

- Results is in steps of 1 second, e.g. from 12:00:30 to 12:00:40 gives 10 measurements, last second not inclusive.
- Dates are expressed in RFC3339 ISO format.

```bash
curl -X GET 'localhost:8080/api/v1/actualCpuUsageByRangeSeconds?start=2022-12-09T12:01:24.429Z&end=2022-12-09T12:01:34.429Z'
```

Successful response: [pastebin](https://pastebin.com/h43MWr6f)

## Notes

- Port-forward with `kubectl port-forward -n kube-prom-stack prometheus-kube-prometheus-stack-prometheus-0 9090`
  and check under `Status > Targets` if the target was scraped successfully.

https://prometheus.io/docs/instrumenting/writing_exporters/#target-labels-not-static-scraped-labels

### Tilt Debugging

- Note that in the Tiltfile the `--continue` option is passed to Delve which instructs the debugger to start the 
process immediately. To instruct Tilt to not continue, remove the option from the tiltfile.

## EMPA deployment notes

- Copy ecoqube-dev.kubeconfig over the charts file of target-exporter: `sudo cp /etc/kubernetes/admin.conf ecoqube-dev.kubeconfig`
- `helm install target-exporter . --namespace target-exporter --create-namespace --values values-empa.yaml`
- Note that some nodes could be tainted for easier testing
