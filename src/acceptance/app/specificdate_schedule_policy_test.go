package app

import (
	"acceptance/config"
	. "acceptance/helpers"
	"bytes"
	"fmt"
	"net/http"

	"strconv"
	"strings"
	"time"

	"github.com/cloudfoundry-incubator/cf-test-helpers/cf"
	"github.com/cloudfoundry-incubator/cf-test-helpers/generator"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gexec"
)

var _ = Describe("AutoScaler specific date schedule policy", func() {
	var (
		appName              string
		appGUID              string
		instanceName         string
		initialInstanceCount int
		location             *time.Location
		startDateTime        time.Time
		endDateTime          time.Time
		policyURL            string
		oauthToken           string
	)

	BeforeEach(func() {

		if cfg.GetServiceOffering() {
			instanceName = generator.PrefixedRandomName("autoscaler", "service")
			createService := cf.Cf("create-service", cfg.ServiceName, cfg.ServicePlan, instanceName).Wait(cfg.DefaultTimeoutDuration())
			Expect(createService).To(Exit(0), "failed creating service")
		}

		initialInstanceCount = 1
		appName = generator.PrefixedRandomName("autoscaler", "nodeapp")
		countStr := strconv.Itoa(initialInstanceCount)
		createApp := cf.Cf("push", appName, "--no-start", "-i", countStr, "-b", cfg.NodejsBuildpackName, "-m", "128M", "-p", config.NODE_APP, "-d", cfg.AppsDomain).Wait(cfg.CfPushTimeoutDuration())
		Expect(createApp).To(Exit(0), "failed creating app")

		guid := cf.Cf("app", appName, "--guid").Wait(cfg.DefaultTimeoutDuration())
		Expect(guid).To(Exit(0))
		appGUID = strings.TrimSpace(string(guid.Out.Contents()))
	})

	AfterEach(func() {
		Expect(cf.Cf("delete", appName, "-f", "-r").Wait(cfg.DefaultTimeoutDuration())).To(Exit(0))

		if cfg.GetServiceOffering() {
			deleteService := cf.Cf("delete-service", instanceName, "-f").Wait(cfg.DefaultTimeoutDuration())
			Expect(deleteService).To(Exit(0))
		}
	})

	Context("when scaling by specific date schedule ", func() {

		JustBeforeEach(func() {

			Expect(cf.Cf("start", appName).Wait(cfg.CfPushTimeoutDuration())).To(Exit(0))
			WaitForNInstancesRunning(appGUID, initialInstanceCount, cfg.DefaultTimeoutDuration())

			location, _ = time.LoadLocation("GMT")
			timeNowInTimeZoneWithOffset := time.Now().In(location).Add(70 * time.Second).Truncate(time.Minute)
			startDateTime = timeNowInTimeZoneWithOffset
			endDateTime = timeNowInTimeZoneWithOffset.Add(time.Duration(interval+120) * time.Second)
			policyStr := GenerateDynamicAndSpecificDateSchedulePolicy(cfg, 1, 4, 80, "GMT", startDateTime, endDateTime, 2, 5, 3)

			if cfg.GetServiceOffering() {
				bindService := cf.Cf("bind-service", appName, instanceName, "-c", policyStr).Wait(cfg.DefaultTimeoutDuration())
				Expect(bindService).To(Exit(0), "failed binding service to app with a policy ")
			} else {
				oauthToken = OauthToken(cfg)
				policyURL = fmt.Sprintf("%s%s", cfg.ASApiEndpoint, strings.Replace(PolicyPath, "{appId}", appGUID, -1))
				req, err := http.NewRequest("PUT", policyURL, bytes.NewBuffer([]byte(policyStr)))
				req.Header.Add("Authorization", oauthToken)
				req.Header.Add("Content-Type", "application/json")

				resp, err := DoAPIRequest(req)
				Expect(err).ShouldNot(HaveOccurred())
				defer resp.Body.Close()
				Expect(resp.StatusCode == 200 || resp.StatusCode == 201).Should(BeTrue())
			}
		})

		AfterEach(func() {
			if cfg.GetServiceOffering() {
				unbindService := cf.Cf("unbind-service", appName, instanceName).Wait(cfg.DefaultTimeoutDuration())
				Expect(unbindService).To(Exit(0), "failed unbinding service from app")
			}
		})

		It("should scale", func() {
			By("setting to initial_min_instance_count")
			jobRunTime := startDateTime.Add(1 * time.Minute).Sub(time.Now())
			WaitForNInstancesRunning(appGUID, 3, jobRunTime)

			By("setting to schedule's instance_min_count")
			jobRunTime = endDateTime.Sub(time.Now())
			Eventually(func() int {
				return RunningInstances(appGUID, jobRunTime)
			}, jobRunTime, 15*time.Second).Should(Equal(2))

			jobRunTime = endDateTime.Sub(time.Now())
			Consistently(func() int {
				return RunningInstances(appGUID, jobRunTime)
			}, jobRunTime, 15*time.Second).Should(Equal(2))

			By("setting to default instance_min_count")
			WaitForNInstancesRunning(appGUID, 1, time.Duration(interval+60)*time.Second)

		})

	})

})
