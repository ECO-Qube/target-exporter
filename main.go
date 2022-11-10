package main

import (
  "context"
  "errors"
  "flag"
  "fmt"
  promapi "github.com/prometheus/client_golang/api"
  promapiv1 "github.com/prometheus/client_golang/api/prometheus/v1"
  "go.uber.org/zap"
  "gopkg.in/yaml.v3"
  "k8s.io/client-go/kubernetes"
  "k8s.io/client-go/tools/clientcmd"
  "k8s.io/client-go/util/homedir"
  "os"
  "os/signal"
  "path/filepath"
  "syscall"
  "time"

  . "git.helio.dev/eco-qube/target-exporter/pkg/exporter"
  . "git.helio.dev/eco-qube/target-exporter/pkg/kubeclient"
  . "git.helio.dev/eco-qube/target-exporter/pkg/promclient"
)

const (
  ErrLoadingConfigFile = "error loading config file"
)

var (
  api        *TargetExporter
  kubeclient *Kubeclient
  promclient *Promclient
  cfg        Config
  logger     *zap.Logger

  // Flags
  isCorsEnabled = false
  kubeconfig    = ""
)

func initLogger() {
  logger, _ = zap.NewProduction()
}

func initFlags() {
  // TODO: Make it proper with Cobra and Viper libraries maybe
  flag.BoolVar(&isCorsEnabled, "cors-enabled", isCorsEnabled, "enable CORS for localhost:3000")

  // If homedir is not set, kubeconfig will be set to empty string
  if home := homedir.HomeDir(); home != "" {
    flag.StringVar(&kubeconfig, "kubeconfig", filepath.Join(home, ".kube", "config"),
      "(optional) absolute path to the kubeconfig file")
  } else {
    flag.StringVar(&kubeconfig, "kubeconfig", "", "absolute path to the kubeconfig file")
  }

  flag.Parse()
}

func initCfgFile() {
  if _, err := os.Stat("./config.yaml"); errors.Is(err, os.ErrNotExist) {
    logger.Fatal(fmt.Sprintf("%s: %v", ErrLoadingConfigFile, err))
  }
  file, err := os.ReadFile("./config.yaml")
  if err != nil {
    logger.Fatal(fmt.Sprintf("%s: %v", ErrLoadingConfigFile, err))
  }
  cfg = Config{}
  err = yaml.Unmarshal(file, &cfg)
  if err != nil {
    logger.Fatal(fmt.Sprintf("%s: %v", ErrLoadingConfigFile, err))
  }
}

func initKubeClient() {
  config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
  if err != nil {
    logger.Fatal(fmt.Sprintf("Error building kubeconfig: %s", err.Error()))
  }

  kubeclientset, err := kubernetes.NewForConfig(config)
  if err != nil {
    logger.Fatal(fmt.Sprintf("Error building kubernetes clientset: %s", err.Error()))
  }

  kubeclient = NewKubeClient(kubeclientset, logger)
}

func initPromClient() {
  // Init Prometheus client
  // TODO: Parametrize
  client, err := promapi.NewClient(promapi.Config{
    Address: "http://localhost:9090",
  })
  if err != nil {
    logger.Fatal(fmt.Sprintf("Error building Prometheus client: %s", err.Error()))
  }

  promv1 := promapiv1.NewAPI(client)
  ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
  defer cancel()
  _, warnings, err := promv1.Query(ctx, "up", time.Now(), promapiv1.WithTimeout(5*time.Second))
  if err != nil {
    logger.Fatal(fmt.Sprintf("Error querying Prometheus during init: %v\n", err))
  }
  if len(warnings) > 0 {
    logger.Warn(fmt.Sprintf("Warnings querying Prometheus during init: %v\n", warnings))
  }

  promclient = NewPromClient(promv1, logger)
}

func init() {
  initFlags()
  initLogger()
  initCfgFile()
  initKubeClient()
  initPromClient()

  api = NewTargetExporter(cfg, kubeclient, promclient, logger, isCorsEnabled)
}

func main() {
  ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
  defer stop()

  api.StartMetrics()
  api.StartApi()

  // Listen for the interrupt signal from the OS
  <-ctx.Done()

  // Restore default behavior on the interrupt signal and notify user of shutdown
  stop()
  logger.Info("Shutting down gracefully, press Ctrl+C again to force")

  // The context is used to inform the server it has 30 seconds to finish
  // the request it is currently handling
  ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
  defer cancel()
  if err := api.GetApiServer().Shutdown(ctx); err != nil {
    logger.Fatal(fmt.Sprintf("API server forced to shutdown: %s", err))
  }
  if err := api.GetMetricsServer().Shutdown(ctx); err != nil {
    logger.Fatal(fmt.Sprintf("Metrics server forced to shutdown: %s", err))
  }
  logger.Info("Target Exporter exiting")
}
