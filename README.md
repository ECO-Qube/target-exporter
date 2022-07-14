# target-exporter

Go service that exports a metrics endpoint with the targets for each node.

## Setup

```yaml
helm install target-exporter charts/target-exporter --namespace=target-exporter --create-namespace 
```

to uninstall:

```yaml
helm uninstall -n target-exporter target-exporter
```

See also NOTES.txt

## How does it work

![Overview of ServiceMonitor tagging and related elements](servicemonitor.png)

### Notes 

- Port-forward with `kubectl port-forward -n kube-prom-stack prometheus-kube-prometheus-stack-prometheus-0 9090` 
and check under `Status > Targets` if the target was scraped successfully.

### TODOs

- [x] Prometheus doesn't have permission to scrape resources in namespaces different than its own (kube-prom-stack).
It would be better to place target-exporter into its own namesapce and give Promethes permission to scrape it.
- [x] Create Helm chart
- [ ] Create a metric like `cpu-diff` and create a timeseries per each node with test values.
- [ ] Publish image in GCP registry https://console.cloud.google.com/gcr/images/k8s-ecoqube-development?project=k8s-ecoqube-development
- [ ] Swap out plain Prometheus in TAS cluster for kube-prometheus-stack or just add the Prometheus Operator and deploy service