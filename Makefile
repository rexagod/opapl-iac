.PHONY: validate
validate:
	@echo "Validating served metrics"
	curl localhost:8080/metrics | \
	python3 -c 'import sys; from prometheus_client.openmetrics import parser;list(parser.text_string_to_metric_families(sys.stdin.buffer.read().decode("utf-8")))'

# TODO: Lint Rego stubs with Regal: https://github.com/styraInc/regal
