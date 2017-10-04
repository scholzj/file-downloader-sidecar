package main

import (
	"flag"
	"fmt"
	"os"
	"log"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/kubernetes"
)

func main() {
	log.Print("Starting File Downloader Sidecar controller")

	kubeconfig := ""
	namespace := "default"
	configMapName := "my-config-map"
	downloadPath := "/tmp/downloads"

	flag.StringVar(&kubeconfig, "kubeconfig", kubeconfig, "kubeconfig file")
	flag.StringVar(&namespace, "namespace", namespace, "Kubernetes namespace")
	flag.StringVar(&configMapName, "config-map", configMapName, "Name of the config map with files to download")
	flag.StringVar(&downloadPath, "download-path", downloadPath, "Path where files should be downloaded")
	flag.Parse()

	if kubeconfig == "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	}

	log.Printf("Parameter --kubeconfig set to: %v", kubeconfig)
	log.Printf("Parameter --namespace set to: %v", namespace)
	log.Printf("Parameter --config-map set to: %v", configMapName)
	log.Printf("Parameter --download-path set to: %v", downloadPath)

	var (
		config *rest.Config
		err    error
	)
	if kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating client: %v", err)
		os.Exit(1)
	}
	client := kubernetes.NewForConfigOrDie(config)

	controller := NewController(client, namespace, configMapName, downloadPath)

	// Now let's start the controller
	stop := make(chan struct{})
	defer close(stop)
	go controller.Run(1, stop)

	// Wait forever
	select {}
}