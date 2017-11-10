package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/squat/jupyter-operator/pkg/controller"
	"github.com/squat/jupyter-operator/pkg/tls"
	"github.com/squat/jupyter-operator/version"

	"github.com/Sirupsen/logrus"
	flag "github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func main() {
	flags := struct {
		kubeconfig string
		logLevel   string
		namespace  string
		version    bool
	}{}
	flag.StringVarP(&flags.kubeconfig, "kubeconfig", "k", "", "path to kubeconfig")
	flag.StringVarP(&flags.logLevel, "loglevel", "l", "info", "logging verbosity")
	flag.StringVarP(&flags.namespace, "namespace", "n", metav1.NamespaceAll, "namespace to watch; leave empty to watch all namespaces")
	flag.BoolVarP(&flags.version, "version", "v", false, "print version and exit")
	flag.Parse()

	if len(os.Args) > 1 {
		command := os.Args[1]
		if flags.version || command == "version" {
			fmt.Println(version.Version)
			return
		}
	}

	level, err := logrus.ParseLevel(flags.logLevel)
	if err != nil {
		logrus.Fatalf("%q is not a valid log level", flags.logLevel)
	}
	logrus.SetLevel(level)

	caCert, key, err := tls.NewCACert()
	if err != nil {
		logrus.Fatalf("failed to generate CA certificate: %v", err)
	}

	cfg := controller.Config{
		CACert:     caCert,
		Key:        key,
		Kubeconfig: flags.kubeconfig,
		Namespace:  flags.namespace,
	}

	stop := make(chan struct{})
	go func() {
		sigs := make(chan os.Signal)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		select {
		case sig := <-sigs:
			logrus.Debugf("Received %s, exiting gracefully...", sig)
			close(stop)
		case <-stop:
		}
	}()
	controller := controller.New(cfg)
	controller.Run(stop, 4)
	os.Exit(0)
}
