## Utilize Open Policy Agent Policy Language for Infrastructure as Code (opapl-iac)

### Problem statement

The current [kube-state-metrics]' [custom resource state] featureset is, to an extent, convoluted. It is not
possible to completely understand every configuration construct that defines this featureset even after going
through the documentation, owing to the side-effects stemming from unrelated changes, or due to the presence
of multiple constructs that don't work together as expected under such circumstances.

[kube-state-metrics]: https://github.com/kubernetes/kube-state-metrics
[custom resource state]: https://github.com/kubernetes/kube-state-metrics/blob/main/docs/customresourcestate-metrics.md

As a result, folks have to ["guess"] the behaviour of these constructs to even jot down a basic configuration.
As the repository scales and supports more constructs, so will the abstract behaviours that define them, which
in addition to being cumbersome for the maintainer, have also grown way too complex for the user, who just
wants to specify a couple of labels and generate metrics entailing them.

["guess"]: https://github.com/kubernetes/kube-state-metrics/issues/2041#issuecomment-1595706154

Such a use case should not be this difficult to maintain and support.

### Proposed solution

There have been mulitple suggestions around a solution for this problem. However, this section will talk
about the one that this document is a part of, i.e., using Rego in an IaC (infrastructure-as-code) manner to
define the configuration, and give complete control and freedom back to the user, so they can choose to
extract, format, and manipulate the metric's textual representation as they wish.

The idea is to expose the resolved GVRs (group-version-resources) that were fetched from the cluster to the
user's Rego stub, defined in the configuration. From thereon, users can utilize this data and take advantage of
[Rego's standard library] to print out metrics exactly how they want.

[Rego's standard library]: https://www.openpolicyagent.org/docs/latest/policy-reference/#rego-standard-library

Nonetheless, these metrics _will_ go through a validation check before being served, just so the [OpenMetrics]
standards are not violated.

[OpenMetrics]: https://openmetrics.io/

_(I assume we'll need to port the logic for the aforementioned validation, since the current implementation
that checks for the same is written in [Python]. For now this lives as a target in the Makefile.)_

[Python]: https://github.com/prometheus/client_python/blob/master/prometheus_client/openmetrics/parser.py#L467

### Usage

The configuration requires two fields, one that specifies the GVRs to resolve, and the other one that tells the
parser how to operate on them.

```yaml
groupVersionResource:
  group: "apps"
  version: "v1"
  resource: "deployments"
stub:
  package stub

  printer {
      help := "# HELP foo foo_help";
      type := "# TYPE foo gauge";
      metrics := [sprintf("foo{namespace=\"%s\",name=\"%s\"} %d", [d["metadata"]["namespace"], d["metadata"]["name"], d["spec"]["replicas"]]) | d := input[_]];
      out := sprintf("%s\n%s\n%s", [help, type, concat("\n", metrics)]);
      print(out)
  }
```

This will produce the following metrics.

```yaml
# HELP foo foo_help
# TYPE foo gauge
foo{namespace="kube-system",name="coredns"} 2
foo{namespace="local-path-storage",name="local-path-provisioner"} 1
# EOF
```

#### Simulating CRS featureset entirely by utilizing Rego code stubs

A more-exhaustive [example] is shown below that reproduces all constructs defined in
[`kube-state-metrics/customresourcestate-metrics.md`] using Rego stubs and generates metrics based on that.

[example]: ./examples/ksm-crs.yaml
[`kube-state-metrics/customresourcestate-metrics.md`]: https://github.com/kubernetes/kube-state-metrics/blob/main/docs/customresourcestate-metrics.md#multiple-metricskitchen-sink

```yaml
groupVersionResource:
  group: "apps"
  version: "v1"
  resource: "deployments"
stub: |
    package stub
    import future.keywords.in

    commonLabels := [{"object_type": "native"}] # For objects native to KSM.
    labelsFromPathName := {"name": ["metadata", "name"]}
    printer {
        familyName := "replica_count"
        familyHelp := sprintf("# HELP %s %s", [familyName, "number of replicas available"])
        familyType := sprintf("# TYPE %s %s", [familyName, "gauge"])
        path := ["spec"]
        labelFromKeyRelative := {"k8s": ["selector", "matchLabels"]}
        labelsFromPathRelative := {"desired_count": ["replicas"]}
        valueFromNonRelative := ["status", "availableReplicas"]
        customLabels := array.concat(commonLabels, [{"custom_metric": "yes"}])
        unfurlFields := [["metadata", "labels"], ["metadata", "annotations"]]
        labelFormat := "%s=\"%v\""
        validationRegex := "\\.|/|-" # https://github.com/kubernetes/kube-state-metrics/pull/2004
        resolvedPaths := [deployment[p] |
            deployment := input[_]
            p := path[_]
        ]
        resolvedFormattedLabelsFromPathNonRelative := [sprintf(labelFormat, [regex.replace(k, validationRegex, "_"), o]) |
            some k, v in labelsFromPathName
            o := object.get(input[_], v, false)
        ]
        resolvedFormattedLabelsFromPathRelative := [sprintf(labelFormat, [regex.replace(k, validationRegex, "_"), o]) |
            some k, v in labelsFromPathRelative
            o := object.get(resolvedPaths[_], v, false)
        ]
        resolvedFormattedLabelFromKeyRelative := [sprintf(labelFormat, [regex.replace(kk, validationRegex, "_"), vv]) | 
          some k, v in labelFromKeyRelative
          o := object.get(resolvedPaths[_], v, false)
          some kk, vv in o
          startswith(kk, k)
        ]
        resolvedFormattedCustomLabels := [sprintf(labelFormat, [regex.replace(k, validationRegex, "_"), v]) |
            el := customLabels[_]
            some k, v in el
        ]
            resolvedUnfurlFields := [[o |
            o := object.get(input[_], el, false)
        ] |
            el := unfurlFields[_]
        ]
        formattedResolvedUnfurlFields := [sprintf(labelFormat, [regex.replace(k, validationRegex, "_"), v]) |
            el := resolvedUnfurlFields[_]
            ell := el[_]
            some k, v in ell
        ]
        values := [o | o := object.get(input[_], valueFromNonRelative, false)]
        # Generate metrics: familyName{<labelsFromPathRelative>, <labelFromKeyRelative>, <customLabels>, <unfurlFields>} valueFromNonRelative
        labelSets := [
            resolvedFormattedLabelsFromPathRelative,
            resolvedFormattedLabelFromKeyRelative,
            resolvedFormattedCustomLabels,
            formattedResolvedUnfurlFields,
        ]
        labelSet := [concat(",", arr) |
            arr := labelSets[_]
        ]
        labels := concat(",", labelSet)
        metricSet := [{sprintf("%s{%s}", [familyName, dedup(withDeployment)]): value} | # https://www.openpolicyagent.org/docs/latest/extensions/#custom-built-in-functions-in-go
            some i, v in resolvedFormattedLabelsFromPathNonRelative
            value := values[i]
            withDeployment := concat(",", [v, labels])
        ]
        metrics := [sprintf("%s %d\n", [metric, value]) | some metric,value in metricSet[_]]
        out := sprintf("%s\n%s\n%s", [familyHelp, familyType, concat("", metrics)])
        print(out)
    }
```

This will produce the following metrics.

```yaml
# HELP replica_count number of replicas available
# TYPE replica_count gauge
replica_count{custom_metric="yes",deployment_kubernetes_io_revision="1",desired_count="1",foo="bar",k8s_app="kube-dns",name="coredns",object_type="native"} 2
replica_count{custom_metric="yes",deployment_kubernetes_io_revision="1",desired_count="1",foo="bar",k8s_app="kube-dns",name="local-path-provisioner",object_type="native"} 1
# EOF
```

***

###### This proof-of-concept was done for the [kube-state-metrics](https://github.com/kubernetes/kube-state-metrics) project.
