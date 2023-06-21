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

#### Comparing Rego to Common Expression Language (CEL) for this usecase

The points below are not exhautive, but aim to highlight the contrasting differences between the two while
keeping the targeted usecase in mind.

|Rego  	|CEL  	|
|:-:	|:-:	|
|Designed to define policies that enumerate instances of data that reflect the current state of the system.|Designed to parse, check, and evaluate expressions.
|Supports scalar and composite assignments for variables, including references.|Variables are absent _in the language_, possible explicitly by programmatically supplying values in a data structure to `Program.Eval` (to make it available in the binding environment).
|Supports Python-like array, set, and object comprehensions.|N/A
|Supports `import`ing packages, to reuse existing logic and build on it, including the Python-like `future` construct that enables working with newer functionality.|N/A

It's also worth noting that, in addition to a wide range of macros, both allow execution in a sandboxed environment,
making sure the bindings made available are the only ones that were explicity passed to it.

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

This will produce the following metric.

```yaml
# HELP foo foo_help
# TYPE foo gauge
foo{namespace="kube-system",name="coredns"} 2
foo{namespace="local-path-storage",name="local-path-provisioner"} 1
# EOF
```

***

###### This proof-of-concept was done for the [kube-state-metrics](https://github.com/kubernetes/kube-state-metrics) project.
