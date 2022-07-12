# target-exporter

Go service that exports a metrics endpoint with the targets for each node.

## How does it work

![Overview of ServiceMonitor tagging and related elements](https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/main/Documentation/custom-metrics-elements.png)

### Notes 

- Port-forward with `kubectl port-forward -n kube-prom-stack prometheus-kube-prometheus-stack-prometheus-0 9090` 
and check under `Status > Targets` if the target was scraped successfully.

### TODOs

- [ ] Prometheus doesn't have permission to scrape resources in namespaces different than its own (kube-prom-stack).
It would be better to place target-exporter into its own namesapce and give Promethes permission to scrape it.
- [ ] Create a metric like `cpu-diff` and create a timeseries per each node with test values.
- [ ] Publish image in GCP registry https://console.cloud.google.com/gcr/images/k8s-ecoqube-development?project=k8s-ecoqube-development
- [ ] Swap out plain Prometheus in TAS cluster for kube-prometheus-stack or just add the Prometheus Operator and deploy service