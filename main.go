package main

import (
	"bytes"
	"context"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/topdown"
)

func main() {
	// Define command-line flags.
	kubeconfigPath := *flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	configurationPath := *flag.String("config", "", "Path to the GVR-rego configuration file.")
	portNumber := *flag.Int("port", 8080, "Port number to listen on.")
	flag.Parse()

	// TODO: Remove this.
	configurationPath, err := filepath.Abs("./examples/config.yaml")
	if err != nil {
		klog.Fatalf("failed to resolve absolute path to configuration file: %v", err)
	}

	// Resolve the Kubeconfig.
	if kubeconfigPath == "" {
		kubeconfigPath = os.Getenv("KUBECONFIG")
		if kubeconfigPath == "" {
			kubeconfigPath = os.Getenv("HOME") + "/.kube/config"
		}
	}
	kubeconfig, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		klog.Fatalf("failed to build kubeconfig: %v", err)
	}

	// Resolve the GVR-rego configuration file.
	sanitizedConfigurationPath := filepath.Clean(configurationPath)
	if sanitizedConfigurationPath == "" {
		klog.Fatalln("configuration path is empty")
	}
	configurationData, err := os.ReadFile(sanitizedConfigurationPath)
	if err != nil {
		klog.Fatalf("failed to read configuration file contents: %v", err)
	}
	c := struct {
		schema.GroupVersionResource `yaml:"groupVersionResource"`
		Stub                        string `yaml:"stub"`
	}{}
	err = yaml.Unmarshal(configurationData, &c)
	if err != nil {
		klog.Fatalf("failed to unmarshal configuration file contents: %v", err)
	}

	// Create a client to fetch the specified resource(s).
	dynamicClient, err := dynamic.NewForConfig(kubeconfig)
	if err != nil {
		klog.Fatalf("failed to create dynamic client: %v", err)
	}

	// Serve.
	http.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {

		// Fetch.
		list, err := dynamicClient.Resource(c.GroupVersionResource).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			klog.Errorf("failed to list deployments: %v", err)
		}
		regoInputItems := list.Items
		var buf bytes.Buffer

		// Process.
		run := rego.New(
			rego.Query("data.stub.printer"),
			rego.Module("stub.rego", c.Stub),
			rego.Input(regoInputItems),
			rego.EnablePrintStatements(true),
			rego.PrintHook(topdown.NewPrintHook(&buf)),
		)
		_, err = run.Eval(context.Background())
		if err != nil {
			klog.Fatalf("failed to evaluate rego query: %v", err)
		}

		// Write.
		_, err = w.Write([]byte(buf.String()))
		if err != nil {
			klog.Errorf("failed to write response: %v", err)
		}

		// Check content type (OM conformance).
		if r.Header.Get("Content-Type") != "text/plain" {
			// Respect OpenMetrics standard.
			_, err = w.Write([]byte("# EOF"))
			if err != nil {
				klog.Errorf("failed to write response: %v", err)
			}
		}
	})
	klog.Infof("starting metrics server on port %d", portNumber)
	err = http.ListenAndServe(":"+strconv.Itoa(portNumber), nil)
	if err != nil {
		klog.Fatalf("failed to start web server: %v", err)
	}
}
