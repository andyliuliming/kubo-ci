package api_extensions_test

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"

	. "tests/test_helpers"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
)

var _ = Describe("Api Extensions", func() {
	const (
		systemNamespace                   = "kube-system"
		serviceAccountSpecTemplate        = "fixtures/sa.yml"
		authDelegatorSpecTemplate         = "fixtures/auth-delegator.yml"
		authReaderSpecTemplate            = "fixtures/auth-reader.yml"
		replicationControllerSpecTemplate = "fixtures/rc.yml"
		serviceSpecTemplate               = "fixtures/service.yml"
		apiServiceSpecTemplate            = "fixtures/apiservice.yml"
	)

	var (
		kubectl *KubectlRunner
		session *gexec.Session

		tmpDir                    string
		apiExtensionsNamespace    string
		sampleApiEndpoint         string
		serviceAccountSpec        string
		authDelegatorSpec         string
		authReaderSpec            string
		replicationControllerSpec string
		serviceSpec               string
		apiServiceSpec            string
	)

	templateNamespaceIntoFile := func(tmpDir, path, namespace string) string {
		t, err := template.ParseFiles(path)
		Expect(err).NotTo(HaveOccurred())

		f, err := ioutil.TempFile(tmpDir, filepath.Base(path))
		Expect(err).NotTo(HaveOccurred())
		defer f.Close()

		type templateInfo struct{ Namespace string }
		Expect(t.Execute(f, templateInfo{Namespace: namespace})).To(Succeed())

		return f.Name()
	}

	BeforeEach(func() {
		var err error
		kubectl = NewKubectlRunner()
		apiExtensionsNamespace = kubectl.Namespace()

		tmpDir, err = ioutil.TempDir("", "api-extensions")
		Expect(err).NotTo(HaveOccurred())

		serviceAccountSpec = templateNamespaceIntoFile(tmpDir, serviceAccountSpecTemplate, apiExtensionsNamespace)
		authDelegatorSpec = templateNamespaceIntoFile(tmpDir, authDelegatorSpecTemplate, apiExtensionsNamespace)
		authReaderSpec = templateNamespaceIntoFile(tmpDir, authReaderSpecTemplate, apiExtensionsNamespace)
		replicationControllerSpec = templateNamespaceIntoFile(tmpDir, replicationControllerSpecTemplate, apiExtensionsNamespace)
		serviceSpec = templateNamespaceIntoFile(tmpDir, serviceSpecTemplate, apiExtensionsNamespace)
		apiServiceSpec = templateNamespaceIntoFile(tmpDir, apiServiceSpecTemplate, apiExtensionsNamespace)

		sampleApiEndpoint = "v1alpha1." + apiExtensionsNamespace + ".k8s.io"
		session = kubectl.RunKubectlCommand("create", "namespace", apiExtensionsNamespace)
		Eventually(session).Should(gexec.Exit(0))
	})

	AfterEach(func() {
		kubectl.RunKubectlCommandWithTimeout("delete", "-f", serviceAccountSpec)
		Eventually(kubectl.RunKubectlCommandInNamespace(systemNamespace, "delete", "-f", authDelegatorSpec), "5s", "1s").Should(gexec.Exit(0))
		Eventually(kubectl.RunKubectlCommandInNamespace(systemNamespace, "delete", "-f", authReaderSpec), "5s", "1s").Should(gexec.Exit(0))
		kubectl.RunKubectlCommandWithTimeout("delete", "-f", replicationControllerSpec)
		kubectl.RunKubectlCommandWithTimeout("delete", "-f", serviceSpec)
		kubectl.RunKubectlCommandWithTimeout("delete", "-f", apiServiceSpec)
		kubectl.RunKubectlCommandWithTimeout("delete", "namespace", apiExtensionsNamespace)
		Expect(os.RemoveAll(tmpDir)).To(Succeed())
	})

	It("successfully deploys an api service", func() {
		By("creating the associated service account")
		kubectl.RunKubectlCommandWithTimeout("create", "-f", serviceAccountSpec)

		By("creating the rolebindings for authentication delegation")
		session := kubectl.RunKubectlCommandInNamespace(systemNamespace, "create", "-f", authDelegatorSpec)
		Eventually(session).Should(gexec.Exit(0))
		session = kubectl.RunKubectlCommandInNamespace(systemNamespace, "create", "-f", authReaderSpec)
		Eventually(session).Should(gexec.Exit(0))

		By("creating the service and replication and replication controller")
		kubectl.RunKubectlCommandWithTimeout("create", "-f", replicationControllerSpec)
		kubectl.RunKubectlCommandWithTimeout("create", "-f", serviceSpec)

		By("creating the api service extension")
		kubectl.RunKubectlCommandWithTimeout("create", "-f", apiServiceSpec)

		WaitForPodsToRun(kubectl, "120s")

		By("verifying the api extension has been registered to the cluster")
		var apiServiceResp struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		}

		session = kubectl.RunKubectlCommand("proxy", "--port=8000")
		Eventually(session).Should(gbytes.Say("Starting to serve on"))
		defer session.Kill()
		resp, err := http.Get(fmt.Sprintf("http://localhost:8000/apis/apiregistration.k8s.io/v1beta1/apiservices/%s", sampleApiEndpoint))
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		jsonResp, err := ioutil.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		json.Unmarshal(jsonResp, &apiServiceResp)
		Expect(apiServiceResp.Metadata.Name).To(Equal(sampleApiEndpoint))
	})
})
