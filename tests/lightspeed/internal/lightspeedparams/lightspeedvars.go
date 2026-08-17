package lightspeedparams

import (
	"github.com/openshift-kni/k8sreporter"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// Labels represents the range of labels that can be used for test cases selection.
	Labels = []string{Label}

	// ExpectedAlerts lists the reference PrometheusRule alerts that the RDS must provide.
	ExpectedAlerts = []string{
		"OLSAppServerDown",
		"OLSLLMCallFailuresHigh",
		"OLS5xxErrorRateHigh",
		"OLSOperatorReconcileErrors",
		"OLSResponseLatencyHigh",
	}

	// OLSConfigGVR is the GroupVersionResource of the OLSConfig custom resource. There is no
	// dedicated eco-goinfra builder for OLSConfig, so tests interact with it via the dynamic
	// client, following the pattern used by the cert-manager system tests.
	OLSConfigGVR = schema.GroupVersionResource{
		Group:    "ols.openshift.io",
		Version:  "v1alpha1",
		Resource: "olsconfigs",
	}

	// PrometheusRuleGVR is the GroupVersionResource of the PrometheusRule custom resource.
	PrometheusRuleGVR = schema.GroupVersionResource{
		Group:    "monitoring.coreos.com",
		Version:  "v1",
		Resource: "prometheusrules",
	}

	// ReporterNamespacesToDump tells the reporter from where to collect logs on failure.
	ReporterNamespacesToDump = map[string]string{
		OLSNamespace: OLSNamespace,
	}

	// ReporterCRDsToDump tells the reporter which CRs to dump on failure.
	ReporterCRDsToDump = []k8sreporter.CRData{
		{Cr: &corev1.PodList{}},
	}
)
