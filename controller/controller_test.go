package controller_test

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"

	"io/ioutil"

	"os"

	"github.com/compozed/deployadactyl/config"
	"github.com/compozed/deployadactyl/constants"
	. "github.com/compozed/deployadactyl/controller"
	D "github.com/compozed/deployadactyl/controller/deployer"
	"github.com/compozed/deployadactyl/controller/deployer/bluegreen"
	"github.com/compozed/deployadactyl/controller/deployer/error_finder"
	I "github.com/compozed/deployadactyl/interfaces"
	"github.com/compozed/deployadactyl/logger"
	"github.com/compozed/deployadactyl/mocks"
	"github.com/compozed/deployadactyl/randomizer"
	"github.com/compozed/deployadactyl/state/push"
	"github.com/compozed/deployadactyl/structs"
	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"
	"github.com/op/go-logging"
	"reflect"
)

var _ = Describe("Controller", func() {

	var (
		deployer           *mocks.Deployer
		silentDeployer     *mocks.Deployer
		pushManagerFactory *mocks.PushManagerFactory
		eventManager       *mocks.EventManager
		errorFinder        *mocks.ErrorFinder
		stopController     *mocks.StopController
		startController    *mocks.StartController
		controller         *Controller
		deployment         I.Deployment
		logBuffer          *Buffer

		appName     string
		environment string
		org         string
		space       string
		uuid        string
		byteBody    []byte
		server      *httptest.Server
		response    *bytes.Buffer
	)

	BeforeEach(func() {
		logBuffer = NewBuffer()
		appName = "appName-" + randomizer.StringRunes(10)
		environment = "environment-" + randomizer.StringRunes(10)
		org = "org-" + randomizer.StringRunes(10)
		space = "non-prod"
		uuid = "uuid-" + randomizer.StringRunes(10)

		eventManager = &mocks.EventManager{}
		deployer = &mocks.Deployer{}
		silentDeployer = &mocks.Deployer{}
		pushManagerFactory = &mocks.PushManagerFactory{}
		stopController = &mocks.StopController{}
		startController = &mocks.StartController{}

		errorFinder = &mocks.ErrorFinder{}
		controller = &Controller{
			Deployer:           deployer,
			SilentDeployer:     silentDeployer,
			Log:                logger.DefaultLogger(logBuffer, logging.DEBUG, "api_test"),
			PushManagerFactory: pushManagerFactory,
			StopController:     stopController,
			StartController:    startController,
			EventManager:       eventManager,
			Config:             config.Config{},
			ErrorFinder:        errorFinder,
		}

		environments := map[string]structs.Environment{}
		environments[environment] = structs.Environment{}
		controller.Config.Environments = environments
		bodyByte := []byte("{}")
		response = &bytes.Buffer{}

		deployment = I.Deployment{
			Body:          &bodyByte,
			Type:          I.DeploymentType{},
			CFContext:     I.CFContext{},
			Authorization: I.Authorization{},
		}

	})

	Describe("RunDeploymentViaHttp handler", func() {
		var (
			router        *gin.Engine
			resp          *httptest.ResponseRecorder
			jsonBuffer    *bytes.Buffer
			foundationURL string
		)
		BeforeEach(func() {
			router = gin.New()
			resp = httptest.NewRecorder()
			jsonBuffer = &bytes.Buffer{}

			router.POST("/v2/deploy/:environment/:org/:space/:appName", controller.RunDeploymentViaHttp)

			server = httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
				byteBody, _ = ioutil.ReadAll(req.Body)
				req.Body.Close()
			}))

			silentDeployUrl := server.URL + "/v1/apps/" + os.Getenv("SILENT_DEPLOY_ENVIRONMENT")
			os.Setenv("SILENT_DEPLOY_URL", silentDeployUrl)
		})
		AfterEach(func() {
			server.Close()
		})
		Context("when deployer succeeds", func() {
			It("deploys and returns http.StatusOK", func() {
				foundationURL = fmt.Sprintf("/v2/deploy/%s/%s/%s/%s", environment, org, space, appName)

				req, err := http.NewRequest("POST", foundationURL, jsonBuffer)
				req.Header.Set("Content-Type", "application/zip")

				Expect(err).ToNot(HaveOccurred())

				deployer.DeployCall.Returns.Error = nil
				deployer.DeployCall.Returns.StatusCode = http.StatusOK
				deployer.DeployCall.Write.Output = "deploy success"

				router.ServeHTTP(resp, req)

				Eventually(resp.Code).Should(Equal(http.StatusOK))
				Eventually(resp.Body).Should(ContainSubstring("deploy success"))

				Eventually(deployer.DeployCall.Received.DeploymentInfo.Environment).Should(Equal(environment))
				Eventually(deployer.DeployCall.Received.DeploymentInfo.Org).Should(Equal(org))
				Eventually(deployer.DeployCall.Received.DeploymentInfo.Space).Should(Equal(space))
				Eventually(deployer.DeployCall.Received.DeploymentInfo.AppName).Should(Equal(appName))
			})

			It("does not run silent deploy when environment other than non-prop", func() {
				foundationURL = fmt.Sprintf("/v2/deploy/%s/%s/%s/%s", environment, org, "not-non-prod", appName)

				req, err := http.NewRequest("POST", foundationURL, jsonBuffer)
				req.Header.Set("Content-Type", "application/zip")

				Expect(err).ToNot(HaveOccurred())

				deployer.DeployCall.Returns.Error = nil
				deployer.DeployCall.Returns.StatusCode = http.StatusOK
				deployer.DeployCall.Write.Output = "deploy success"

				router.ServeHTTP(resp, req)

				Eventually(resp.Code).Should(Equal(http.StatusOK))
				Eventually(resp.Body).Should(ContainSubstring("deploy success"))

				Eventually(len(byteBody)).Should(Equal(0))
			})
		})

		Context("when deployer fails", func() {
			It("doesn't deploy and gives http.StatusInternalServerError", func() {
				foundationURL = fmt.Sprintf("/v2/deploy/%s/%s/%s/%s", environment, org, space, appName)

				req, err := http.NewRequest("POST", foundationURL, jsonBuffer)
				req.Header.Set("Content-Type", "application/zip")

				Expect(err).ToNot(HaveOccurred())

				deployer.DeployCall.Returns.Error = errors.New("bork")
				deployer.DeployCall.Returns.StatusCode = http.StatusInternalServerError

				router.ServeHTTP(resp, req)

				Eventually(resp.Code).Should(Equal(http.StatusInternalServerError))
				Eventually(resp.Body).Should(ContainSubstring("bork"))
			})
		})

		Context("when parameters are added to the url", func() {
			It("does not return an error", func() {
				foundationURL = fmt.Sprintf("/v2/deploy/%s/%s/%s/%s?broken=false", environment, org, space, appName)

				req, err := http.NewRequest("POST", foundationURL, jsonBuffer)
				req.Header.Set("Content-Type", "application/zip")

				Expect(err).ToNot(HaveOccurred())

				deployer.DeployCall.Write.Output = "deploy success"
				deployer.DeployCall.Returns.StatusCode = http.StatusOK

				router.ServeHTTP(resp, req)

				Eventually(resp.Code).Should(Equal(http.StatusOK))
				Eventually(resp.Body).Should(ContainSubstring("deploy success"))
			})
		})
	})

	Describe("PutRequestHandler", func() {
		var (
			router     *gin.Engine
			resp       *httptest.ResponseRecorder
			jsonBuffer *bytes.Buffer
		)

		BeforeEach(func() {
			router = gin.New()
			resp = httptest.NewRecorder()
			jsonBuffer = &bytes.Buffer{}

			router.PUT("/v2/deploy/:environment/:org/:space/:appName", controller.PutRequestHandler)

			server = httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
				byteBody, _ = ioutil.ReadAll(req.Body)
				req.Body.Close()
			}))
		})

		AfterEach(func() {
			server.Close()
		})

		Context("when state is set to stopped", func() {
			Context("when stop succeeds", func() {
				It("returns http status.OK", func() {
					foundationURL := fmt.Sprintf("/v2/deploy/%s/%s/%s/%s", environment, org, space, appName)
					jsonBuffer = bytes.NewBufferString(`{"state": "stopped"}`)

					req, err := http.NewRequest("PUT", foundationURL, jsonBuffer)
					req.Header.Set("Content-Type", "application/json")

					Expect(err).ToNot(HaveOccurred())

					router.ServeHTTP(resp, req)

					Eventually(resp.Code).Should(Equal(http.StatusOK))
				})
			})

			It("logs request origination address", func() {
				foundationURL := fmt.Sprintf("/v2/deploy/%s/%s/%s/%s", environment, org, space, appName)
				jsonBuffer = bytes.NewBufferString(`{"state": "stopped"}`)

				req, _ := http.NewRequest("PUT", foundationURL, jsonBuffer)
				req.Header.Set("Content-Type", "application/json")

				router.ServeHTTP(resp, req)

				Eventually(logBuffer).Should(Say("PUT Request originated from"))
			})

			It("calls StopDeployment with a Deployment", func() {
				foundationURL := fmt.Sprintf("/v2/deploy/%s/%s/%s/%s", environment, org, space, appName)
				jsonBuffer = bytes.NewBufferString(`{"state": "stopped"}`)

				req, err := http.NewRequest("PUT", foundationURL, jsonBuffer)
				req.Header.Set("Content-Type", "application/json")

				Expect(err).ToNot(HaveOccurred())

				router.ServeHTTP(resp, req)

				Expect(stopController.StopDeploymentCall.Received.Deployment).ToNot(BeNil())
			})

			It("calls StopDeployment with correct CFContext", func() {
				foundationURL := fmt.Sprintf("/v2/deploy/%s/%s/%s/%s", environment, org, space, appName)
				jsonBuffer = bytes.NewBufferString(`{"state": "stopped"}`)

				req, err := http.NewRequest("PUT", foundationURL, jsonBuffer)
				req.Header.Set("Content-Type", "application/json")

				Expect(err).ToNot(HaveOccurred())

				router.ServeHTTP(resp, req)

				cfContext := stopController.StopDeploymentCall.Received.Deployment.CFContext
				Expect(cfContext.Environment).To(Equal(environment))
				Expect(cfContext.Space).To(Equal(space))
				Expect(cfContext.Organization).To(Equal(org))
				Expect(cfContext.Application).To(Equal(appName))
			})

			It("calls StopDeployment with correct authorization", func() {
				foundationURL := fmt.Sprintf("/v2/deploy/%s/%s/%s/%s", environment, org, space, appName)
				jsonBuffer = bytes.NewBufferString(`{"state": "stopped"}`)

				req, err := http.NewRequest("PUT", foundationURL, jsonBuffer)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Basic bXlVc2VyOm15UGFzc3dvcmQ=")

				Expect(err).ToNot(HaveOccurred())

				router.ServeHTTP(resp, req)

				auth := stopController.StopDeploymentCall.Received.Deployment.Authorization
				Expect(auth.Username).To(Equal("myUser"))
				Expect(auth.Password).To(Equal("myPassword"))
			})

			It("writes the process output to the response", func() {
				foundationURL := fmt.Sprintf("/v2/deploy/%s/%s/%s/%s", environment, org, space, appName)
				jsonBuffer = bytes.NewBufferString(`{"state": "stopped"}`)

				req, err := http.NewRequest("PUT", foundationURL, jsonBuffer)
				req.Header.Set("Content-Type", "application/json")

				Expect(err).ToNot(HaveOccurred())

				stopController.StopDeploymentCall.Writes = "this is the process output"
				router.ServeHTTP(resp, req)

				bytes, _ := ioutil.ReadAll(resp.Body)
				Expect(string(bytes)).To(ContainSubstring("this is the process output"))
			})

			It("passes the data in the request body", func() {
				foundationURL := fmt.Sprintf("/v2/deploy/%s/%s/%s/%s", environment, org, space, appName)
				jsonBuffer = bytes.NewBufferString(`{"state": "stopped", "data": {"user_id": "jhodo", "group": "XP_IS_CHG" }}`)

				req, err := http.NewRequest("PUT", foundationURL, jsonBuffer)
				req.Header.Set("Content-Type", "application/json")

				Expect(err).ToNot(HaveOccurred())

				router.ServeHTTP(resp, req)

				Expect(stopController.StopDeploymentCall.Received.Data["user_id"]).To(Equal("jhodo"))
				Expect(stopController.StopDeploymentCall.Received.Data["group"]).To(Equal("XP_IS_CHG"))
			})

			Context("if requested state is not 'stop'", func() {
				It("does not call StopDeployment", func() {
					foundationURL := fmt.Sprintf("/v2/deploy/%s/%s/%s/%s", environment, org, space, appName)
					jsonBuffer = bytes.NewBufferString(`{"state": "started"}`)

					req, err := http.NewRequest("PUT", foundationURL, jsonBuffer)
					req.Header.Set("Content-Type", "application/json")

					Expect(err).ToNot(HaveOccurred())

					router.ServeHTTP(resp, req)

					Expect(stopController.StopDeploymentCall.Called).To(Equal(false))
				})
			})
		})

		Context("when state is set to started", func() {
			It("calls StartDeployment with a Deployment", func() {
				foundationURL := fmt.Sprintf("/v2/deploy/%s/%s/%s/%s", environment, org, space, appName)
				jsonBuffer = bytes.NewBufferString(`{"state": "started"}`)

				req, err := http.NewRequest("PUT", foundationURL, jsonBuffer)
				req.Header.Set("Content-Type", "application/json")

				Expect(err).ToNot(HaveOccurred())

				router.ServeHTTP(resp, req)

				Expect(startController.StartDeploymentCall.Received.Deployment).ToNot(BeNil())
			})

			It("calls StartDeployment with correct CFContext", func() {
				foundationURL := fmt.Sprintf("/v2/deploy/%s/%s/%s/%s", environment, org, space, appName)
				jsonBuffer = bytes.NewBufferString(`{"state": "started"}`)

				req, err := http.NewRequest("PUT", foundationURL, jsonBuffer)
				req.Header.Set("Content-Type", "application/json")

				Expect(err).ToNot(HaveOccurred())

				router.ServeHTTP(resp, req)

				cfContext := startController.StartDeploymentCall.Received.Deployment.CFContext
				Expect(cfContext.Environment).To(Equal(environment))
				Expect(cfContext.Space).To(Equal(space))
				Expect(cfContext.Organization).To(Equal(org))
				Expect(cfContext.Application).To(Equal(appName))
			})

			It("calls StartDeployment with correct authorization", func() {
				foundationURL := fmt.Sprintf("/v2/deploy/%s/%s/%s/%s", environment, org, space, appName)
				jsonBuffer = bytes.NewBufferString(`{"state": "started"}`)

				req, err := http.NewRequest("PUT", foundationURL, jsonBuffer)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Basic bXlVc2VyOm15UGFzc3dvcmQ=")

				Expect(err).ToNot(HaveOccurred())

				router.ServeHTTP(resp, req)

				auth := startController.StartDeploymentCall.Received.Deployment.Authorization
				Expect(auth.Username).To(Equal("myUser"))
				Expect(auth.Password).To(Equal("myPassword"))
			})

			Context("if requested state is not 'start'", func() {
				It("does not call StartDeployment", func() {
					foundationURL := fmt.Sprintf("/v2/deploy/%s/%s/%s/%s", environment, org, space, appName)
					jsonBuffer = bytes.NewBufferString(`{"state": "stopped"}`)

					req, err := http.NewRequest("PUT", foundationURL, jsonBuffer)
					req.Header.Set("Content-Type", "application/json")

					Expect(err).ToNot(HaveOccurred())

					router.ServeHTTP(resp, req)

					Expect(startController.StartDeploymentCall.Called).To(Equal(false))
				})
			})
		})
	})

	Describe("RunDeployment", func() {
		Context("when verbose deployer is called", func() {
			It("deployer is provided correct authorization", func() {

				deployer.DeployCall.Returns.Error = nil
				deployer.DeployCall.Returns.StatusCode = http.StatusOK
				deployer.DeployCall.Write.Output = "little-timmy-env.zip"

				response := &bytes.Buffer{}

				deployment := &I.Deployment{
					Body: &[]byte{},
					Authorization: I.Authorization{
						Username: "username",
						Password: "password",
					},
					CFContext: I.CFContext{
						Environment:  environment,
						Organization: org,
						Space:        space,
						Application:  appName,
					},
				}
				deployment.Type.ZIP = true
				deployResponse := controller.RunDeployment(deployment, response)

				Eventually(deployer.DeployCall.Called).Should(Equal(1))
				Eventually(silentDeployer.DeployCall.Called).Should(Equal(0))

				Eventually(deployResponse.StatusCode).Should(Equal(http.StatusOK))

				Eventually(deployer.DeployCall.Received.DeploymentInfo.Username).Should(Equal(deployment.Authorization.Username))
				Eventually(deployer.DeployCall.Received.DeploymentInfo.Password).Should(Equal(deployment.Authorization.Password))
			})

			It("deployer is provided the body", func() {

				deployer.DeployCall.Returns.Error = nil
				deployer.DeployCall.Returns.StatusCode = http.StatusOK
				deployer.DeployCall.Write.Output = "little-timmy-env.zip"

				response := &bytes.Buffer{}

				bodyBytes := []byte("a test body string")

				deployment := &I.Deployment{
					Body: &bodyBytes,
					Authorization: I.Authorization{
						Username: "username",
						Password: "password",
					},
					CFContext: I.CFContext{
						Environment:  environment,
						Organization: org,
						Space:        space,
						Application:  appName,
					},
				}
				deployment.Type.ZIP = true

				deployResponse := controller.RunDeployment(deployment, response)
				receivedBody, _ := ioutil.ReadAll(deployer.DeployCall.Received.DeploymentInfo.Body)
				Eventually(deployer.DeployCall.Called).Should(Equal(1))
				Eventually(silentDeployer.DeployCall.Called).Should(Equal(0))

				Eventually(deployResponse.StatusCode).Should(Equal(http.StatusOK))

				Eventually(receivedBody).Should(Equal(*deployment.Body))
			})

			It("channel resolves when no errors occur", func() {
				deployment.CFContext.Environment = environment
				deployment.CFContext.Organization = org
				deployment.CFContext.Space = space
				deployment.CFContext.Application = appName
				deployment.Type.ZIP = true

				deployer.DeployCall.Returns.Error = nil
				deployer.DeployCall.Returns.StatusCode = http.StatusOK
				deployer.DeployCall.Write.Output = "little-timmy-env.zip"

				deployResponse := controller.RunDeployment(&deployment, response)

				Eventually(deployer.DeployCall.Called).Should(Equal(1))
				Eventually(silentDeployer.DeployCall.Called).Should(Equal(0))

				Eventually(deployResponse.StatusCode).Should(Equal(http.StatusOK))

				Eventually(deployer.DeployCall.Received.DeploymentInfo.ContentType).Should(Equal("ZIP"))
				Eventually(deployer.DeployCall.Received.DeploymentInfo.Environment).Should(Equal(environment))
				Eventually(deployer.DeployCall.Received.DeploymentInfo.Org).Should(Equal(org))
				Eventually(deployer.DeployCall.Received.DeploymentInfo.Space).Should(Equal(space))
				Eventually(deployer.DeployCall.Received.DeploymentInfo.AppName).Should(Equal(appName))

				ret, _ := ioutil.ReadAll(response)
				Eventually(string(ret)).Should(Equal("little-timmy-env.zip"))
			})

			It("channel resolves when errors occur", func() {
				deployment.CFContext.Environment = environment
				deployment.CFContext.Organization = org
				deployment.CFContext.Space = space
				deployment.CFContext.Application = appName
				deployment.Type.ZIP = true

				deployer.DeployCall.Returns.Error = errors.New("bork")
				deployer.DeployCall.Returns.StatusCode = http.StatusInternalServerError
				deployer.DeployCall.Write.Output = "little-timmy-env.zip"

				deployResponse := controller.RunDeployment(&deployment, response)

				Eventually(deployer.DeployCall.Called).Should(Equal(1))
				Eventually(silentDeployer.DeployCall.Called).Should(Equal(0))

				Eventually(deployResponse.StatusCode).Should(Equal(http.StatusInternalServerError))
				Eventually(deployResponse.Error.Error()).Should(Equal("bork"))

				Eventually(deployer.DeployCall.Received.DeploymentInfo.ContentType).Should(Equal("ZIP"))
				Eventually(deployer.DeployCall.Received.DeploymentInfo.Environment).Should(Equal(environment))
				Eventually(deployer.DeployCall.Received.DeploymentInfo.Org).Should(Equal(org))
				Eventually(deployer.DeployCall.Received.DeploymentInfo.Space).Should(Equal(space))
				Eventually(deployer.DeployCall.Received.DeploymentInfo.AppName).Should(Equal(appName))

				ret, _ := ioutil.ReadAll(response)
				Eventually(string(ret)).Should(Equal("little-timmy-env.zip"))
			})

			It("does not set the basic auth header if no credentials are passed", func() {
				deployer.DeployCall.Write.Output = "little-timmy-env.zip"

				response := &bytes.Buffer{}

				deployment := &I.Deployment{
					Body: &[]byte{},
					Type: I.DeploymentType{ZIP: true},
					CFContext: I.CFContext{
						Environment:  environment,
						Organization: org,
						Space:        space,
						Application:  appName,
					},
					Authorization: I.Authorization{
						Username: "",
						Password: "",
					},
				}
				controller.RunDeployment(deployment, response)

				Eventually(deployer.DeployCall.Received.DeploymentInfo.Username).Should(Equal(""))
				Eventually(deployer.DeployCall.Received.DeploymentInfo.Password).Should(Equal(""))
			})

			It("sets the basic auth header if credentials are passed", func() {
				deployment.CFContext.Environment = environment
				deployment.Type.ZIP = true

				deployer.DeployCall.Write.Output = "little-timmy-env.zip"

				deployment.Authorization = I.Authorization{
					Username: "TestUsername",
					Password: "TestPassword",
				}

				controller.RunDeployment(&deployment, response)

				Eventually(deployer.DeployCall.Received.DeploymentInfo.Username).Should(Equal("TestUsername"))
				Eventually(deployer.DeployCall.Received.DeploymentInfo.Password).Should(Equal("TestPassword"))
			})
		})

		Context("when SILENT_DEPLOY_ENVIRONMENT is true", func() {
			It("channel resolves true when no errors occur", func() {
				deployment.CFContext.Environment = environment
				deployment.CFContext.Organization = org
				deployment.CFContext.Space = space
				deployment.CFContext.Application = appName
				deployment.Type.ZIP = true

				os.Setenv("SILENT_DEPLOY_ENVIRONMENT", environment)
				deployer.DeployCall.Returns.Error = nil
				deployer.DeployCall.Returns.StatusCode = http.StatusOK
				deployer.DeployCall.Write.Output = "little-timmy-env.zip"

				deployResponse := controller.RunDeployment(&deployment, response)

				Eventually(deployer.DeployCall.Called).Should(Equal(1))
				Eventually(silentDeployer.DeployCall.Called).Should(Equal(1))

				Eventually(deployResponse.StatusCode).Should(Equal(http.StatusOK))

				Eventually(deployer.DeployCall.Received.DeploymentInfo.ContentType).Should(Equal("ZIP"))
				Eventually(deployer.DeployCall.Received.DeploymentInfo.Environment).Should(Equal(environment))
				Eventually(deployer.DeployCall.Received.DeploymentInfo.Org).Should(Equal(org))
				Eventually(deployer.DeployCall.Received.DeploymentInfo.Space).Should(Equal(space))
				Eventually(deployer.DeployCall.Received.DeploymentInfo.AppName).Should(Equal(appName))

				ret, _ := ioutil.ReadAll(response)
				Eventually(string(ret)).Should(Equal("little-timmy-env.zip"))
			})
			It("channel resolves when no errors occur", func() {
				deployment.CFContext.Environment = environment
				deployment.CFContext.Organization = org
				deployment.CFContext.Space = space
				deployment.CFContext.Application = appName
				deployment.Type.ZIP = true

				os.Setenv("SILENT_DEPLOY_ENVIRONMENT", environment)
				deployer.DeployCall.Returns.Error = nil
				deployer.DeployCall.Returns.StatusCode = http.StatusOK
				deployer.DeployCall.Write.Output = "little-timmy-env.zip"

				silentDeployer.DeployCall.Returns.Error = errors.New("bork")
				silentDeployer.DeployCall.Returns.StatusCode = http.StatusInternalServerError

				silentDeployUrl := server.URL + "/v1/apps/" + os.Getenv("SILENT_DEPLOY_ENVIRONMENT")
				os.Setenv("SILENT_DEPLOY_URL", silentDeployUrl)

				deployResponse := controller.RunDeployment(&deployment, response)

				Eventually(deployer.DeployCall.Called).Should(Equal(1))
				Eventually(silentDeployer.DeployCall.Called).Should(Equal(1))

				Eventually(deployResponse.StatusCode).Should(Equal(http.StatusOK))

				Eventually(deployer.DeployCall.Received.DeploymentInfo.ContentType).Should(Equal("ZIP"))
				Eventually(deployer.DeployCall.Received.DeploymentInfo.Environment).Should(Equal(environment))
				Eventually(deployer.DeployCall.Received.DeploymentInfo.Org).Should(Equal(org))
				Eventually(deployer.DeployCall.Received.DeploymentInfo.Space).Should(Equal(space))
				Eventually(deployer.DeployCall.Received.DeploymentInfo.AppName).Should(Equal(appName))

				ret, _ := ioutil.ReadAll(response)
				Eventually(string(ret)).Should(Equal("little-timmy-env.zip"))
			})
		})

		Context("when called", func() {
			It("logs building deploymentInfo", func() {
				deployment.CFContext.Environment = environment

				controller.RunDeployment(&deployment, response)
				Eventually(logBuffer).Should(Say("building deploymentInfo"))
			})
			It("creates a pusher creator", func() {
				deployment.CFContext.Environment = environment
				deployment.Type.ZIP = true

				controller.RunDeployment(&deployment, response)
				Eventually(pushManagerFactory.PusherCreatorCall.Called).Should(Equal(true))

			})
			It("Provides body for pusher creator", func() {
				bodyByte := []byte("body string")
				deployment.CFContext.Environment = environment
				deployment.Body = &bodyByte
				deployment.Type.ZIP = true

				controller.RunDeployment(&deployment, response)
				returnedBody, _ := ioutil.ReadAll(pushManagerFactory.PusherCreatorCall.Received.DeployEventData.RequestBody)
				Eventually(returnedBody).Should(Equal(bodyByte))
			})
			It("Provides response for pusher creator", func() {
				deployment.CFContext.Environment = environment
				deployment.Type.ZIP = true

				response = bytes.NewBuffer([]byte("hello"))

				controller.RunDeployment(&deployment, response)
				returnedResponse, _ := ioutil.ReadAll(pushManagerFactory.PusherCreatorCall.Received.DeployEventData.Response)
				Eventually(returnedResponse).Should(Equal([]byte("hello")))
			})
			Context("when type is JSON", func() {
				It("gets the artifact url from the request", func() {
					bodyByte := []byte("{\"artifact_url\": \"the artifact url\"}")
					deployment.Body = &bodyByte
					deployment.CFContext.Environment = environment
					deployment.Type.JSON = true

					controller.RunDeployment(&deployment, response)
					Eventually(pushManagerFactory.PusherCreatorCall.Received.DeployEventData.DeploymentInfo.ArtifactURL).Should(Equal("the artifact url"))
				})
				It("gets the manifest from the request", func() {
					bodyByte := []byte("{\"artifact_url\": \"the artifact url\", \"manifest\": \"the manifest\"}")
					deployment.Body = &bodyByte
					deployment.CFContext.Environment = environment
					deployment.Type.JSON = true

					controller.RunDeployment(&deployment, response)
					Eventually(pushManagerFactory.PusherCreatorCall.Received.DeployEventData.DeploymentInfo.Manifest).Should(Equal("the manifest"))
				})
				It("gets the data from the request", func() {
					bodyByte := []byte("{\"artifact_url\": \"the artifact url\", \"data\": {\"avalue\": \"the data\"}}")
					deployment.Body = &bodyByte
					deployment.CFContext.Environment = environment
					deployment.Type.JSON = true

					controller.RunDeployment(&deployment, response)
					Eventually(pushManagerFactory.PusherCreatorCall.Received.DeployEventData.DeploymentInfo.Data["avalue"]).Should(Equal("the data"))
				})
			})
			Context("the deployment info", func() {
				Context("when environment does not exist", func() {
					It("returns an error with StatusInternalServerError", func() {
						deployment.CFContext.Environment = "bad env"
						deployment.Type.ZIP = true

						deploymentResponse := controller.RunDeployment(&deployment, response)
						Eventually(deploymentResponse.Error).Should(HaveOccurred())
						Eventually(reflect.TypeOf(deploymentResponse.Error)).Should(Equal(reflect.TypeOf(D.EnvironmentNotFoundError{})))
					})
				})
				Context("when environment exists", func() {
					Context("when Authorization doesn't have values", func() {
						Context("and authentication is not required", func() {
							It("returns username and password from the config", func() {
								deployment.CFContext.Environment = environment
								deployment.Type.ZIP = true

								deployment.Authorization.Username = ""
								deployment.Authorization.Password = ""
								controller.Config.Username = "username-" + randomizer.StringRunes(10)
								controller.Config.Password = "password-" + randomizer.StringRunes(10)

								controller.RunDeployment(&deployment, response)

								Eventually(pushManagerFactory.PusherCreatorCall.Received.DeployEventData.DeploymentInfo.Username).Should(Equal(controller.Config.Username))
								Eventually(pushManagerFactory.PusherCreatorCall.Received.DeployEventData.DeploymentInfo.Password).Should(Equal(controller.Config.Password))
							})
						})
						Context("and authentication is required", func() {
							It("returns an error", func() {
								deployment.CFContext.Environment = environment
								deployment.Type.ZIP = true

								deployment.Authorization.Username = ""
								deployment.Authorization.Password = ""

								controller.Config.Environments[environment] = structs.Environment{
									Authenticate: true,
								}

								deploymentResponse := controller.RunDeployment(&deployment, response)

								Eventually(deploymentResponse.Error).Should(HaveOccurred())
								Eventually(deploymentResponse.Error.Error()).Should(Equal("basic auth header not found"))
							})
						})
					})
					Context("when Authorization has values", func() {
						It("logs checking auth", func() {
							deployment.CFContext.Environment = environment
							deployment.Type.ZIP = true

							controller.RunDeployment(&deployment, response)

							Eventually(logBuffer).Should(Say("checking for basic auth"))
						})
						It("returns username and password from the authorization", func() {
							deployment.CFContext.Environment = environment
							deployment.Type.ZIP = true

							deployment.Authorization.Username = "username-" + randomizer.StringRunes(10)
							deployment.Authorization.Password = "password-" + randomizer.StringRunes(10)

							controller.RunDeployment(&deployment, response)

							Eventually(pushManagerFactory.PusherCreatorCall.Received.DeployEventData.DeploymentInfo.Username).Should(Equal(deployment.Authorization.Username))
							Eventually(pushManagerFactory.PusherCreatorCall.Received.DeployEventData.DeploymentInfo.Password).Should(Equal(deployment.Authorization.Password))
						})
					})
					It("has the correct org, space ,appname, env, uuid", func() {
						deployment.CFContext.Environment = environment
						deployment.Type.ZIP = true

						deployment.CFContext.Organization = org
						deployment.CFContext.Space = space
						deployment.CFContext.Application = appName
						deployment.CFContext.Environment = environment

						controller.RunDeployment(&deployment, response)

						Eventually(pushManagerFactory.PusherCreatorCall.Received.DeployEventData.DeploymentInfo.Org).Should(Equal(org))
						Eventually(pushManagerFactory.PusherCreatorCall.Received.DeployEventData.DeploymentInfo.Space).Should(Equal(space))
						Eventually(pushManagerFactory.PusherCreatorCall.Received.DeployEventData.DeploymentInfo.AppName).Should(Equal(appName))
						Eventually(pushManagerFactory.PusherCreatorCall.Received.DeployEventData.DeploymentInfo.Environment).Should(Equal(environment))
					})
					It("has the correct JSON content type", func() {
						deployment.CFContext.Environment = environment
						deployment.Type.JSON = true
						bodyByte := []byte(`{"artifact_url": "xyz"}`)
						deployment.Body = &bodyByte

						controller.RunDeployment(&deployment, response)

						Eventually(pushManagerFactory.PusherCreatorCall.Received.DeployEventData.DeploymentInfo.ContentType).Should(Equal("JSON"))
					})
					It("has the correct ZIP content type", func() {
						deployment.CFContext.Environment = environment
						deployment.Type.ZIP = true

						controller.RunDeployment(&deployment, response)

						Eventually(pushManagerFactory.PusherCreatorCall.Received.DeployEventData.DeploymentInfo.ContentType).Should(Equal("ZIP"))
					})
					It("has the correct body", func() {
						deployment.CFContext.Environment = environment
						deployment.Type.ZIP = true
						bodyByte := []byte(`{"artifact_url": "xyz"}`)
						deployment.Body = &bodyByte

						controller.RunDeployment(&deployment, response)

						returnedBody, _ := ioutil.ReadAll(pushManagerFactory.PusherCreatorCall.Received.DeployEventData.DeploymentInfo.Body)
						Eventually(string(returnedBody)).Should(Equal(string(bodyByte)))
					})

					Context("when contentType is neither", func() {
						It("returns an error", func() {
							deployment.CFContext.Environment = environment

							deployResponse := controller.RunDeployment(&deployment, response)

							Eventually(reflect.TypeOf(deployResponse.Error)).Should(Equal(reflect.TypeOf(D.InvalidContentTypeError{})))
						})
					})

					Context("when uuid is not provided", func() {
						It("creates a new uuid", func() {
							deployment.CFContext.Environment = environment
							deployment.Type.ZIP = true

							controller.RunDeployment(&deployment, response)

							Eventually(pushManagerFactory.PusherCreatorCall.Received.DeployEventData.DeploymentInfo.UUID).ShouldNot(BeEmpty())
						})
					})
					Context("when uuid is provided", func() {
						It("uses the provided uuid", func() {
							deployment.CFContext.Environment = environment
							uuid := randomizer.StringRunes(10)
							deployment.CFContext.UUID = uuid
							deployment.Type.ZIP = true

							controller.RunDeployment(&deployment, response)

							Eventually(pushManagerFactory.PusherCreatorCall.Received.DeployEventData.DeploymentInfo.UUID).Should(Equal(uuid))

						})
					})
					It("has the correct domain and skipssl", func() {
						deployment.CFContext.Environment = environment
						domain := "domain-" + randomizer.StringRunes(10)
						deployment.Authorization.Username = ""
						deployment.Authorization.Password = ""
						deployment.Type.ZIP = true

						controller.Config.Environments[environment] = structs.Environment{
							Domain:  domain,
							SkipSSL: true,
						}

						controller.RunDeployment(&deployment, response)

						Eventually(pushManagerFactory.PusherCreatorCall.Received.DeployEventData.DeploymentInfo.Domain).Should(Equal(domain))
						Eventually(pushManagerFactory.PusherCreatorCall.Received.DeployEventData.DeploymentInfo.SkipSSL).Should(BeTrue())
					})
					It("has correct custom parameters", func() {

						deployment.CFContext.Environment = environment
						deployment.Type.ZIP = true

						customParams := make(map[string]interface{})
						customParams["param1"] = "value1"
						customParams["param2"] = "value2"

						controller.Config.Environments[environment] = structs.Environment{
							CustomParams: customParams,
						}

						controller.RunDeployment(&deployment, response)

						Eventually(pushManagerFactory.PusherCreatorCall.Received.DeployEventData.DeploymentInfo.CustomParams["param1"]).Should(Equal("value1"))
						Eventually(pushManagerFactory.PusherCreatorCall.Received.DeployEventData.DeploymentInfo.CustomParams["param2"]).Should(Equal("value2"))

					})
					It("is passed to the pusher creator", func() {
						deployment.CFContext.Environment = environment
						deployment.Type.ZIP = true

						controller.RunDeployment(&deployment, response)

						Eventually(pushManagerFactory.PusherCreatorCall.Received.DeployEventData.DeploymentInfo).ShouldNot(BeNil())
					})

					It("correctly extracts artifact url from body", func() {
						artifactURL := "artifactURL-" + randomizer.StringRunes(10)
						bodyByte := []byte(fmt.Sprintf(`{"artifact_url": "%s"}`, artifactURL))

						deployment.CFContext.Environment = environment
						deployment.Body = &bodyByte
						deployment.Type.JSON = true

						controller.RunDeployment(&deployment, response)

						Eventually(pushManagerFactory.PusherCreatorCall.Received.DeployEventData.DeploymentInfo.ArtifactURL).Should(Equal(artifactURL))
					})
					Context("if artifact url isn't provided in body", func() {
						It("returns an error", func() {
							bodyByte := []byte("{}")

							deployment.CFContext.Environment = environment
							deployment.Body = &bodyByte
							deployment.Type.JSON = true

							deploymentResponse := controller.RunDeployment(&deployment, response)

							Eventually(deploymentResponse.Error).ShouldNot(BeNil())
							Eventually(deploymentResponse.Error.Error()).Should(ContainSubstring("The following properties are missing: artifact_url"))
						})
					})
					Context("if body is invalid", func() {
						It("returns an error", func() {
							bodyByte := []byte("")

							deployment.CFContext.Environment = environment
							deployment.Body = &bodyByte
							deployment.Type.JSON = true

							deploymentResponse := controller.RunDeployment(&deployment, response)

							Eventually(deploymentResponse.Error).ShouldNot(BeNil())
							Eventually(deploymentResponse.Error.Error()).Should(ContainSubstring("EOF"))
						})
					})
					Context("deploy.start event", func() {
						It("logs a start event", func() {
							deployment.CFContext.Environment = environment
							deployment.Type.ZIP = true

							controller.RunDeployment(&deployment, response)

							Eventually(logBuffer).Should(Say("emitting a deploy.start event"))
						})
						It("calls Emit", func() {
							deployment.CFContext.Environment = environment
							deployment.Type.ZIP = true

							controller.RunDeployment(&deployment, response)

							Expect(eventManager.EmitCall.Received.Events[0].Type).Should(Equal(constants.DeployStartEvent))
						})
						It("calls EmitEvent", func() {
							deployment.CFContext.Environment = environment
							deployment.Type.ZIP = true

							controller.RunDeployment(&deployment, response)

							Expect(eventManager.EmitEventCall.Received.Events[0].Name()).Should(Equal("DeployStartedEvent"))
						})
						Context("when Emit fails", func() {
							It("returns error", func() {
								deployment.CFContext.Environment = environment
								deployment.Type.ZIP = true

								eventManager.EmitCall.Returns.Error = []error{errors.New("a test error")}

								deploymentResponse := controller.RunDeployment(&deployment, response)

								Expect(reflect.TypeOf(deploymentResponse.Error)).Should(Equal(reflect.TypeOf(D.EventError{})))
							})
						})
						Context("when EmitEvent fails", func() {
							It("returns error", func() {
								deployment.CFContext.Environment = environment
								deployment.Type.ZIP = true

								eventManager.EmitEventCall.Returns.Error = []error{errors.New("a test error")}

								deploymentResponse := controller.RunDeployment(&deployment, response)

								Expect(reflect.TypeOf(deploymentResponse.Error)).Should(Equal(reflect.TypeOf(D.EventError{})))
							})
						})
						It("passes populated deploymentInfo to DeployStartEvent event", func() {
							deployment.CFContext.Environment = environment
							deployment.CFContext.Application = appName
							deployment.CFContext.Space = space
							deployment.CFContext.Organization = org
							deployment.Type.ZIP = true

							controller.RunDeployment(&deployment, response)

							deploymentInfo := eventManager.EmitCall.Received.Events[0].Data.(*structs.DeployEventData).DeploymentInfo
							Expect(deploymentInfo.AppName).To(Equal(appName))
							Expect(deploymentInfo.Org).To(Equal(org))
							Expect(deploymentInfo.Space).To(Equal(space))
							Expect(deploymentInfo.UUID).ToNot(BeNil())
						})
						It("passes CFContext to EmitEvent in the event", func() {
							deployment.CFContext.Environment = environment
							deployment.CFContext.Application = appName
							deployment.CFContext.Space = space
							deployment.CFContext.Organization = org
							deployment.CFContext.UUID = uuid

							deployment.Type.ZIP = true

							controller.RunDeployment(&deployment, response)

							event := eventManager.EmitEventCall.Received.Events[0].(push.DeployStartedEvent)
							Expect(event.CFContext.Environment).To(Equal(environment))
							Expect(event.CFContext.Application).To(Equal(appName))
							Expect(event.CFContext.Space).To(Equal(space))
							Expect(event.CFContext.Organization).To(Equal(org))
							Expect(event.CFContext.UUID).To(Equal(uuid))
						})
						It("passes Auth to EmitEvent in the event", func() {
							deployment.CFContext.Environment = environment
							deployment.Authorization = I.Authorization{
								Username: "myuser",
								Password: "mypassword",
							}

							deployment.Type.ZIP = true

							controller.RunDeployment(&deployment, response)

							event := eventManager.EmitEventCall.Received.Events[0].(push.DeployStartedEvent)
							Expect(event.Auth.Username).To(Equal("myuser"))
							Expect(event.Auth.Password).To(Equal("mypassword"))
						})
						It("passes other info to EmitEvent", func() {
							deployment.CFContext.Environment = environment
							deployment.Authorization = I.Authorization{
								Username: "myuser",
								Password: "mypassword",
							}

							controller.Config.Environments[environment] = structs.Environment{
								Name: environment,
							}

							deployment.Type.ZIP = true

							controller.RunDeployment(&deployment, response)

							event := eventManager.EmitEventCall.Received.Events[0].(push.DeployStartedEvent)
							Expect(event.Body).ToNot(BeNil())
							Expect(event.ContentType).To(Equal("ZIP"))
							Expect(event.Environment.Name).To(Equal(environment))
							Expect(event.Response).ToNot(BeNil())
						})
					})
					It("emits a finished event", func() {
						deployment.CFContext.Environment = environment
						deployment.Type.ZIP = true

						controller.RunDeployment(&deployment, response)
						Expect(eventManager.EmitCall.Received.Events[2].Type).Should(Equal(constants.DeployFinishEvent))
					})
					Context("when finished emit fails", func() {
						It("returns error", func() {
							deployment.CFContext.Environment = environment
							deployment.Type.ZIP = true

							eventManager.EmitCall.Returns.Error = []error{nil, nil, errors.New("a test error")}

							deploymentResponse := controller.RunDeployment(&deployment, response)

							Expect(reflect.TypeOf(deploymentResponse.Error)).Should(Equal(reflect.TypeOf(bluegreen.FinishDeployError{})))
						})
					})
					It("emits a success event", func() {
						deployment.CFContext.Environment = environment
						deployment.Type.ZIP = true

						controller.RunDeployment(&deployment, response)
						Expect(eventManager.EmitCall.Received.Events[1].Type).Should(Equal(constants.DeploySuccessEvent))
					})
					Context("when emit successOrFailure fails", func() {
						It("logs an error", func() {
							deployment.CFContext.Environment = environment
							deployment.Type.ZIP = true

							eventManager.EmitCall.Returns.Error = []error{nil, errors.New("a test error"), nil}

							controller.RunDeployment(&deployment, response)
							Eventually(logBuffer).Should(Say("an error occurred when emitting a deploy.success event"))
						})
					})
					It("emits a failure event", func() {
						deployment.CFContext.Environment = environment
						deployment.Type.ZIP = true

						eventManager.EmitCall.Returns.Error = []error{errors.New("a test error"), nil, nil}

						controller.RunDeployment(&deployment, response)
						Expect(eventManager.EmitCall.Received.Events[1].Type).Should(Equal(constants.DeployFailureEvent))
					})
					It("logs emitting an event", func() {
						deployment.CFContext.Environment = environment
						deployment.Type.ZIP = true

						controller.RunDeployment(&deployment, response)
						Eventually(logBuffer).Should(Say("emitting a deploy.success event"))
					})
					It("prints found errors to the response", func() {
						deployment.CFContext.Environment = environment
						deployment.Type.ZIP = true

						eventManager.EmitCall.Returns.Error = []error{errors.New("a test error"), nil, nil}

						retError := error_finder.CreateLogMatchedError("a description", []string{"some details"}, "a solution", "a code")
						errorFinder.FindErrorsCall.Returns.Errors = []I.LogMatchedError{retError}

						controller.RunDeployment(&deployment, response)
						responseBytes, _ := ioutil.ReadAll(response)
						Eventually(string(responseBytes)).Should(ContainSubstring("The following error was found in the above logs: a description"))
						Eventually(string(responseBytes)).Should(ContainSubstring("Error: some details"))
						Eventually(string(responseBytes)).Should(ContainSubstring("Potential solution: a solution"))
					})
					It("passes populated deploymentInfo to DeploySuccessEvent event", func() {
						deployment.CFContext.Environment = environment
						deployment.CFContext.Application = appName
						deployment.CFContext.Space = space
						deployment.CFContext.Organization = org
						deployment.Type.ZIP = true

						controller.RunDeployment(&deployment, response)

						deploymentInfo := eventManager.EmitCall.Received.Events[1].Data.(*structs.DeployEventData).DeploymentInfo
						Expect(deploymentInfo.AppName).To(Equal(appName))
						Expect(deploymentInfo.Org).To(Equal(org))
						Expect(deploymentInfo.Space).To(Equal(space))
						Expect(deploymentInfo.UUID).ToNot(BeNil())
					})
					It("passes populated deploymentInfo to DeployFinishEvent event", func() {
						deployment.CFContext.Environment = environment
						deployment.CFContext.Application = appName
						deployment.CFContext.Space = space
						deployment.CFContext.Organization = org
						deployment.Type.ZIP = true

						controller.RunDeployment(&deployment, response)

						deploymentInfo := eventManager.EmitCall.Received.Events[2].Data.(*structs.DeployEventData).DeploymentInfo
						Expect(deploymentInfo.AppName).To(Equal(appName))
						Expect(deploymentInfo.Org).To(Equal(org))
						Expect(deploymentInfo.Space).To(Equal(space))
						Expect(deploymentInfo.UUID).ToNot(BeNil())
					})
				})
			})

		})

	})

})
