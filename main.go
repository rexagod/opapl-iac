package main

import (
	"bytes"
	"context"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/types"
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
	var kubeconfigPath string
	var configurationPath string
	var portNumber int
	flag.StringVar(&kubeconfigPath, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&configurationPath, "config", "./examples/config.yaml", "Path to the GVR-rego configuration file.")
	flag.IntVar(&portNumber, "port", 8080, "Port number to listen on.")
	flag.Parse()

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
	absoluteFilepath, err := filepath.Abs(configurationPath)
	if err != nil {
		klog.Fatalf("failed to resolve absolute path: %v", err)
	}
	sanitizedConfigurationPath := filepath.Clean(absoluteFilepath)
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
			rego.Function1(
				&rego.Function{
					Name: "dedup",
					Decl: types.NewFunction(types.Args(types.S), types.S),
				},
				func(bctx rego.BuiltinContext, op1 *ast.Term) (*ast.Term, error) {
					// Deduplicate keys.
					seen := map[string]string{}
					arr := strings.Split(string(op1.Value.(ast.String)), ",")
					for _, labelKV := range arr {
						kv := strings.Split(labelKV, "=")
						seen[kv[0]] = labelKV
					}
					var deduped []string
					for _, v := range seen {
						deduped = append(deduped, v)
					}
					// Sort keys for determinism.
					sort.Strings(deduped)
					return ast.StringTerm(strings.Join(deduped, ",")), nil
				},
			),
			rego.Module("stub.rego", c.Stub),
			rego.Input(regoInputItems),
			rego.EnablePrintStatements(true),
			rego.PrintHook(topdown.NewPrintHook(&buf)),
		)
		stub, err := run.PrepareForEval(context.Background())
		if err != nil {
			klog.Fatalf("failed to prepare for evaluation: %v", err)
		}
		_, err = stub.Eval(context.Background())
		if err != nil {
			klog.Fatalf("failed to evaluate rego query: %v", err)
		}

		// Write.
		buf.Truncate(buf.Len() - 1) // Remove trailing newline.
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
