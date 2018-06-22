package creator

import (
	"os"

	"reflect"
	"runtime"

	"bytes"

	"github.com/compozed/deployadactyl/config"
	"github.com/compozed/deployadactyl/eventmanager/handlers/healthchecker"
	I "github.com/compozed/deployadactyl/interfaces"
	"github.com/compozed/deployadactyl/mocks"
	"github.com/compozed/deployadactyl/state"
	"github.com/compozed/deployadactyl/state/push"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Custom creator", func() {

	var path string

	BeforeEach(func() {
		path = os.Getenv("PATH")
		var newpath string
		dir, _ := os.Getwd()
		if runtime.GOOS == "windows" {
			newpath = dir + "\\..\\bin;" + path
		} else {
			newpath = dir + "/../bin:" + path
		}
		os.Setenv("PATH", newpath)
	})

	AfterEach(func() {
		os.Unsetenv("CF_USERNAME")
		os.Unsetenv("CF_PASSWORD")
		os.Setenv("PATH", path)
	})

	It("creates the creator from the provided yaml configuration", func() {

		os.Setenv("CF_USERNAME", "test user")
		os.Setenv("CF_PASSWORD", "test pwd")

		level := "DEBUG"
		configPath := "./testconfig.yml"

		creator, err := Custom(level, configPath, CreatorModuleProvider{})

		Expect(err).ToNot(HaveOccurred())
		Expect(creator.config).ToNot(BeNil())
		Expect(creator.eventManager).ToNot(BeNil())
		Expect(creator.fileSystem).ToNot(BeNil())
		Expect(creator.logger).ToNot(BeNil())
		Expect(creator.writer).ToNot(BeNil())
	})

	It("fails due to lack of required env variables", func() {
		level := "DEBUG"
		configPath := "./testconfig.yml"

		_, err := Custom(level, configPath, CreatorModuleProvider{})

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("missing environment variables: CF_USERNAME, CF_PASSWORD"))
	})

	Describe("CreateAuthResolver", func() {

		Context("when mock constructor is provided", func() {
			It("should return the mock implementation", func() {
				os.Setenv("CF_USERNAME", "test user")
				os.Setenv("CF_PASSWORD", "test pwd")

				level := "DEBUG"
				configPath := "./testconfig.yml"

				expected := &mocks.AuthResolver{}
				creator, _ := Custom(level, configPath, CreatorModuleProvider{
					NewAuthResolver: func(authConfig config.Config) I.AuthResolver {
						return expected
					},
				})
				resolver := creator.CreateAuthResolver()
				Expect(reflect.TypeOf(resolver)).To(Equal(reflect.TypeOf(expected)))
			})
		})

		Context("when mock constructor is not provided", func() {
			It("should return the default implementation", func() {
				os.Setenv("CF_USERNAME", "")
				os.Setenv("CF_PASSWORD", "")

				level := "DEBUG"
				configPath := "./testconfig.yml"

				creator, _ := Custom(level, configPath, CreatorModuleProvider{})
				resolver := creator.CreateAuthResolver()
				Expect(reflect.TypeOf(resolver)).To(Equal(reflect.TypeOf(state.AuthResolver{})))
				concrete := resolver.(state.AuthResolver)
				Expect(concrete.Config).ToNot(BeNil())
			})
		})

	})

	Describe("CreateHealthChecker", func() {
		Context("when mock constructor is not provided", func() {
			It("should return the default implementation", func() {
				os.Setenv("CF_USERNAME", "test user")
				os.Setenv("CF_PASSWORD", "test pwd")

				level := "DEBUG"
				configPath := "./testconfig.yml"

				creator, _ := Custom(level, configPath, CreatorModuleProvider{})
				actual := creator.CreateHealthChecker()

				Expect(reflect.TypeOf(actual)).To(Equal(reflect.TypeOf(healthchecker.HealthChecker{})))

				Expect(actual.OldURL).To(Equal("api.cf"))
				Expect(actual.NewURL).To(Equal("apps"))
				Expect(actual.SilentDeployURL).ToNot(BeNil())
				Expect(actual.SilentDeployEnvironment).ToNot(BeNil())
				Expect(actual.Client).ToNot(BeNil())
			})
		})
	})

	Describe("CreateRequestCreator", func() {
		Context("when the provided request is a PostDeploymentRequest", func() {
			Context("when mock constructor is provided", func() {
				It("should return the mock implementation", func() {
					os.Setenv("CF_USERNAME", "test user")
					os.Setenv("CF_PASSWORD", "test pwd")

					level := "DEBUG"
					configPath := "./testconfig.yml"

					expected := &mocks.RequestCreator{}
					creator, _ := Custom(level, configPath, CreatorModuleProvider{
						NewPushRequestCreator: func(creator Creator, uuid string, request I.PostDeploymentRequest, buffer *bytes.Buffer) I.RequestCreator {
							return expected
						},
					})
					rc, _ := creator.CreateRequestCreator("the uuid", I.PostDeploymentRequest{}, bytes.NewBuffer([]byte{}))
					Expect(rc).To(Equal(expected))
				})
			})

			Context("when mock constructor is not provided", func() {
				It("should return the default implementation", func() {
					os.Setenv("CF_USERNAME", "test user")
					os.Setenv("CF_PASSWORD", "test pwd")

					level := "DEBUG"
					configPath := "./testconfig.yml"

					response := bytes.NewBuffer([]byte("the response"))
					request := I.PostDeploymentRequest{
						Deployment: I.Deployment{
							CFContext: I.CFContext{
								Organization: "the org",
							},
						},
					}

					creator, _ := Custom(level, configPath, CreatorModuleProvider{})
					rc, _ := creator.CreateRequestCreator("the uuid", request, response)

					Expect(reflect.TypeOf(rc)).To(Equal(reflect.TypeOf(&PushRequestCreator{})))
					concrete := rc.(*PushRequestCreator)
					Expect(concrete.Creator).To(Equal(creator))
					Expect(concrete.Buffer).To(Equal(response))
					Expect(concrete.Request).To(Equal(request))
					Expect(concrete.Log.UUID).To(Equal("the uuid"))
				})

			})
		})

		Context("when the provided request is a PutDeploymentRequest", func() {
			Context("and requested state is stopped", func() {
				Context("when mock constructor is provided", func() {
					It("should return the mock implementation", func() {
						os.Setenv("CF_USERNAME", "test user")
						os.Setenv("CF_PASSWORD", "test pwd")

						level := "DEBUG"
						configPath := "./testconfig.yml"

						expected := &mocks.RequestCreator{}
						creator, _ := Custom(level, configPath, CreatorModuleProvider{
							NewStopRequestCreator: func(creator Creator, uuid string, request I.PutDeploymentRequest, buffer *bytes.Buffer) I.RequestCreator {
								return expected
							},
						})
						rc, _ := creator.CreateRequestCreator("the uuid", I.PutDeploymentRequest{Request: I.PutRequest{State: "stopped"}}, bytes.NewBuffer([]byte{}))
						Expect(rc).To(Equal(expected))
					})
				})

				Context("when mock constructor is not provided", func() {
					It("should return the default implementation", func() {
						os.Setenv("CF_USERNAME", "test user")
						os.Setenv("CF_PASSWORD", "test pwd")

						level := "DEBUG"
						configPath := "./testconfig.yml"

						response := bytes.NewBuffer([]byte("the response"))
						request := I.PutDeploymentRequest{
							Deployment: I.Deployment{
								CFContext: I.CFContext{
									Organization: "the org",
								},
							},
							Request: I.PutRequest{
								State: "stopped",
							},
						}

						creator, _ := Custom(level, configPath, CreatorModuleProvider{})
						rc, _ := creator.CreateRequestCreator("the uuid", request, response)

						Expect(reflect.TypeOf(rc)).To(Equal(reflect.TypeOf(&StopRequestCreator{})))
						concrete := rc.(*StopRequestCreator)
						Expect(concrete.Creator).To(Equal(creator))
						Expect(concrete.Buffer).To(Equal(response))
						Expect(concrete.Request).To(Equal(request))
						Expect(concrete.Log.UUID).To(Equal("the uuid"))
					})

				})
			})

			Context("and requested state is started", func() {
				Context("when mock constructor is provided", func() {
					It("should return the mock implementation", func() {
						os.Setenv("CF_USERNAME", "test user")
						os.Setenv("CF_PASSWORD", "test pwd")

						level := "DEBUG"
						configPath := "./testconfig.yml"

						expected := &mocks.RequestCreator{}
						creator, _ := Custom(level, configPath, CreatorModuleProvider{
							NewStartRequestCreator: func(creator Creator, uuid string, request I.PutDeploymentRequest, buffer *bytes.Buffer) I.RequestCreator {
								return expected
							},
						})
						rc, _ := creator.CreateRequestCreator("the uuid", I.PutDeploymentRequest{Request: I.PutRequest{State: "started"}}, bytes.NewBuffer([]byte{}))
						Expect(rc).To(Equal(expected))
					})
				})

				Context("when mock constructor is not provided", func() {
					It("should return the default implementation", func() {
						os.Setenv("CF_USERNAME", "test user")
						os.Setenv("CF_PASSWORD", "test pwd")

						level := "DEBUG"
						configPath := "./testconfig.yml"

						response := bytes.NewBuffer([]byte("the response"))
						request := I.PutDeploymentRequest{
							Deployment: I.Deployment{
								CFContext: I.CFContext{
									Organization: "the org",
								},
							},
							Request: I.PutRequest{
								State: "started",
							},
						}

						creator, _ := Custom(level, configPath, CreatorModuleProvider{})
						rc, _ := creator.CreateRequestCreator("the uuid", request, response)

						Expect(reflect.TypeOf(rc)).To(Equal(reflect.TypeOf(&StartRequestCreator{})))
						concrete := rc.(*StartRequestCreator)
						Expect(concrete.Creator).To(Equal(creator))
						Expect(concrete.Buffer).To(Equal(response))
						Expect(concrete.Request).To(Equal(request))
						Expect(concrete.Log.UUID).To(Equal("the uuid"))
					})

				})
			})
		})

		Context("when the provided request is unknown", func() {
			It("returns an error", func() {
				os.Setenv("CF_USERNAME", "test user")
				os.Setenv("CF_PASSWORD", "test pwd")

				level := "DEBUG"
				configPath := "./testconfig.yml"

				response := bytes.NewBuffer([]byte("the response"))

				creator, _ := Custom(level, configPath, CreatorModuleProvider{})
				_, err := creator.CreateRequestCreator("the uuid", "", response)

				Expect(err).To(HaveOccurred())
				Expect(reflect.TypeOf(err)).To(Equal(reflect.TypeOf(InvalidRequestError{})))
			})
		})
	})

	Describe("CreateRequestProcessor", func() {
		Context("when mock constructor is provided", func() {
			It("should return the mock implementation", func() {
				os.Setenv("CF_USERNAME", "test user")
				os.Setenv("CF_PASSWORD", "test pwd")

				level := "DEBUG"
				configPath := "./testconfig.yml"

				expected := &mocks.RequestProcessor{}
				creator, _ := Custom(level, configPath, CreatorModuleProvider{
					NewPushRequestProcessor: func(log I.DeploymentLogger, controller I.PushController, request I.PostDeploymentRequest, buffer *bytes.Buffer) I.RequestProcessor {
						return expected
					},
				})
				processor := creator.CreateRequestProcessor("the uuid", I.PostDeploymentRequest{}, bytes.NewBuffer([]byte{}))
				Expect(processor).To(Equal(expected))
			})
		})

		Context("when mock constructor is not provided", func() {
			It("should return the default implementation", func() {
				os.Setenv("CF_USERNAME", "test user")
				os.Setenv("CF_PASSWORD", "test pwd")

				level := "DEBUG"
				configPath := "./testconfig.yml"

				response := bytes.NewBuffer([]byte("the response"))
				request := I.PostDeploymentRequest{
					Deployment: I.Deployment{
						CFContext: I.CFContext{
							Organization: "the org",
						},
					},
				}

				creator, _ := Custom(level, configPath, CreatorModuleProvider{})
				processor := creator.CreateRequestProcessor("the uuid", request, response)

				Expect(reflect.TypeOf(processor)).To(Equal(reflect.TypeOf(&push.PushRequestProcessor{})))
				concrete := processor.(*push.PushRequestProcessor)
				Expect(concrete.PushController).ToNot(BeNil())
				Expect(concrete.Response).To(Equal(response))
				Expect(concrete.Request).To(Equal(request))
				Expect(concrete.Log.UUID).To(Equal("the uuid"))
			})

		})

		Context("when an unknown request is provided", func() {
			It("returns an InvalidRequestProcessor", func() {
				os.Setenv("CF_USERNAME", "test user")
				os.Setenv("CF_PASSWORD", "test pwd")

				level := "DEBUG"
				configPath := "./testconfig.yml"

				response := bytes.NewBuffer([]byte("the response"))

				request := ""

				creator, _ := Custom(level, configPath, CreatorModuleProvider{})
				processor := creator.CreateRequestProcessor("the uuid", request, response)

				Expect(reflect.TypeOf(processor)).To(Equal(reflect.TypeOf(InvalidRequestProcessor{})))
			})
		})
	})

})
