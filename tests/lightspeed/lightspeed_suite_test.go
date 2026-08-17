package lightspeed

import (
	"runtime"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/reportxml"
	"github.com/rh-ecosystem-edge/eco-gotests/tests/internal/reporter"
	. "github.com/rh-ecosystem-edge/eco-gotests/tests/lightspeed/internal/lightspeedinittools"
	"github.com/rh-ecosystem-edge/eco-gotests/tests/lightspeed/internal/lightspeedparams"
	_ "github.com/rh-ecosystem-edge/eco-gotests/tests/lightspeed/tests"
)

var _, currentFile, _, _ = runtime.Caller(0)

func TestLightspeed(t *testing.T) {
	_, reporterConfig := GinkgoConfiguration()
	reporterConfig.JUnitReport = LightspeedConfig.GetJunitReportPath(currentFile)

	RegisterFailHandler(Fail)
	RunSpecs(t, "Lightspeed", Label(lightspeedparams.Labels...), reporterConfig)
}

var _ = JustAfterEach(func() {
	reporter.ReportIfFailed(
		CurrentSpecReport(), currentFile, lightspeedparams.ReporterNamespacesToDump, lightspeedparams.ReporterCRDsToDump)
})

var _ = ReportAfterSuite("", func(report Report) {
	reportxml.Create(
		report, LightspeedConfig.GetReportPath(), LightspeedConfig.TCPrefix)
})
