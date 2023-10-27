package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"git.helio.dev/eco-qube/target-exporter/pkg/pyzhm"
	. "git.helio.dev/eco-qube/target-exporter/pkg/scheduling"
	"git.helio.dev/eco-qube/target-exporter/pkg/serverswitch"
	promapi "github.com/prometheus/client_golang/api"
	promapiv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	. "git.helio.dev/eco-qube/target-exporter/pkg/infrastructure"
	. "git.helio.dev/eco-qube/target-exporter/pkg/kubeclient"
	. "git.helio.dev/eco-qube/target-exporter/pkg/promclient"
)

const (
	ErrLoadingConfigFile = "error loading config file"
)

var (
	orchestrator   *Orchestrator
	api            *TargetExporter
	kubeclient     *Kubeclient
	promclient     *Promclient
	pyzhmClient    *pyzhm.PyzhmClient
	metricsSrv     *http.Server
	bootCfg        Config
	logger         *zap.Logger
	serverSwitches map[string]*serverswitch.IpmiServerSwitch

	// Flags
	config            = "config.yaml"
	debug             = false
	isCorsDisabled    = false
	kubeconfig        = ""
	promclientAddress = ""
	pyzhmAddress      = ""
)

func initLogger() {
	if debug {
		logger, _ = zap.NewDevelopment()
	} else {
		logger, _ = zap.NewProduction()
	}
}

func initFlags() {
	flag.StringVar(&config, "config", config, "configuration file location")

	flag.BoolVar(&debug, "debug", debug, "debug mode")

	// TODO: Make it proper with Cobra and Viper libraries maybe
	flag.BoolVar(&isCorsDisabled, "cors-disabled", isCorsDisabled, "disable CORS for localhost:3000")

	// If homedir is not set, kubeconfig will be set to empty string
	if home := homedir.HomeDir(); home != "" {
		flag.StringVar(&kubeconfig, "kubeconfig", filepath.Join(home, ".kube", "config"),
			"(optional) absolute path to the kubeconfig file")
	} else {
		flag.StringVar(&kubeconfig, "kubeconfig", "", "absolute path to the kubeconfig file")
	}

	flag.StringVar(&promclientAddress, "promclient-address", "http://localhost:9090", "Prometheus Address for querying")
	flag.StringVar(&pyzhmAddress, "pyzhm-address", "http://localhost:9090", "PyZHM Address")

	flag.Parse()
}

func initCfgFile() {
	if _, err := os.Stat(config); errors.Is(err, os.ErrNotExist) {
		logger.Fatal(fmt.Sprintf("%s: %v", ErrLoadingConfigFile, err))
	}
	file, err := os.ReadFile(config)
	if err != nil {
		logger.Fatal(fmt.Sprintf("%s: %v", ErrLoadingConfigFile, err))
	}
	bootCfg = Config{}
	err = yaml.Unmarshal(file, &bootCfg)
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
	client, err := promapi.NewClient(promapi.Config{
		Address: promclientAddress,
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

func initServerOnOff() {
	serverSwitches = make(map[string]*serverswitch.IpmiServerSwitch)
	// For each server in the bootconfig, create a server switch
	for k, v := range bootCfg.BmcNodeMappings {
		sswitch, err := serverswitch.NewIpmiServerSwitch(v, bootCfg.BmcUsername, bootCfg.BmcPassword, logger)
		if err != nil {
			logger.Error(fmt.Sprintf("Error creating server switch: %s", err.Error()))
			continue
		}
		serverSwitches[k] = sswitch
	}
}

func initMetricsServer() {
	metricsSrv = &http.Server{
		Addr:    ":2112",
		Handler: promhttp.Handler(),
	}
}

func initOrchestrator() {
	strategy := NewServerOnOffStrategy(serverSwitches, promclient, logger)
	// TODO: Add switch to turn on/off to the dashboard
	strategy.Start()

	orchestrator = NewOrchestrator(
		kubeclient,
		promclient,
		pyzhmClient,
		logger,
		api.Targets(),
		api.Schedulable(),
		strategy,
		bootCfg.PyzhmNodeMappings,
		bootCfg.Setpoints,
	)
}

// checkConfig checks if the config is valid, in particular it makes sure that the node names specified in the
// config are valid node names in the cluster
func checkConfig() {
	invalidNames := make([]string, 0)
	for k, _ := range bootCfg.Targets {
		if !kubeclient.IsNodeNameValid(k) {
			invalidNames = append(invalidNames, k)
		}
	}
	if len(invalidNames) > 0 {
		logger.Fatal(fmt.Sprintf("The following node names are not valid: %s. Are they reachable from target-exporter?",
			strings.Join(invalidNames, ", ")))
	}
}

func init() {
	initFlags()
	initLogger()
	initCfgFile()
	initKubeClient()

	checkConfig()

	initMetricsServer()
	initPromClient()
	initPyzhmClient()

	api = NewTargetExporter(
		promclient,
		kubeclient,
		pyzhmClient,
		metricsSrv,
		bootCfg,
		isCorsDisabled,
		logger,
	)
}

func initPyzhmClient() {
	pyzhmClient = pyzhm.NewPyzhmClient(logger, pyzhmAddress)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	api.StartMetrics()
	api.StartApi()
	initServerOnOff()
	initOrchestrator()
	api.SetOrchestrator(orchestrator)
	automaticJobSpawn := NewAutomaticJobSpawn(orchestrator, kubeclient, promclient, logger)
	api.SetAutomaticJobSpawn(automaticJobSpawn)

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
