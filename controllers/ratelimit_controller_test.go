package controllers

import (
	"context"
	"sync"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/uuid"
	"sigs.k8s.io/controller-runtime/pkg/client"

	limitadorv1alpha1 "github.com/3scale/limitador-operator/api/v1alpha1"
)

var _ = Describe("RateLimit controller", func() {
	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	// Used to generate a different limit on every test so they don't collide.
	var newRateLimit = func() limitadorv1alpha1.RateLimit {
		// The name can't start with a number.
		name := "a" + string(uuid.NewUUID())

		return limitadorv1alpha1.RateLimit{
			TypeMeta: metav1.TypeMeta{
				Kind:       "RateLimit",
				APIVersion: "limitador.3scale.net/v1alpha1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
			},
			Spec: limitadorv1alpha1.RateLimitSpec{
				Conditions: []string{"req.method == GET"},
				MaxValue:   10,
				Namespace:  "test-namespace",
				Seconds:    60,
				Variables:  []string{"user_id"},
			},
		}
	}

	// The next couple of functions are useful to verify that an HTTP request is
	// made after a call to the kubernetesClient.
	// The functions are wrappers for k8sClient.Create and k8sClient.Delete, so
	// the signature is the same.
	// We know that after creating, deleting, etc. a RateLimit CR, an HTTP
	// request is made to create, delete, etc. the limit in Limitador. These
	// functions are useful for waiting until the state is synchronized.

	// Wraps a function with the same signature as k8sClient.Create and waits
	// for an HTTP request.
	var runCreateAndWaitHTTPReq = func(f func(ctx context.Context,
		object client.Object,
		opts ...client.CreateOption,
	) error) func(ctx context.Context, object client.Object, opts ...client.CreateOption) error {
		return func(ctx context.Context, object client.Object, opts ...client.CreateOption) error {
			reqsAtStart := len(mockedHTTPServer.ReceivedRequests())

			err := f(ctx, object, opts...)
			if err != nil {
				return err
			}

			Eventually(func() bool {
				return len(mockedHTTPServer.ReceivedRequests()) > reqsAtStart
			}, timeout, interval).Should(BeTrue())

			return nil
		}
	}

	// Wraps a function with the same signature as k8sClient.Delete and waits
	// for an HTTP request.
	var runDeleteAndWaitHTTPReq = func(f func(ctx context.Context,
		object client.Object,
		opts ...client.DeleteOption,
	) error) func(ctx context.Context, object client.Object, opts ...client.DeleteOption) error {
		return func(ctx context.Context, object client.Object, opts ...client.DeleteOption) error {
			reqsAtStart := len(mockedHTTPServer.ReceivedRequests())

			err := f(ctx, object, opts...)
			if err != nil {
				return err
			}

			Eventually(func() bool {
				return len(mockedHTTPServer.ReceivedRequests()) > reqsAtStart
			}, timeout, interval).Should(BeTrue())

			return nil
		}
	}

	var addHandlerForLimitCreation = func(limitSpecJson string) {
		mockedHTTPServer.AppendHandlers(
			ghttp.CombineHandlers(
				ghttp.VerifyRequest("POST", "/limits"),
				ghttp.VerifyJSON(limitSpecJson),
			),
		)
	}

	var addHandlerForLimitDeletion = func(limitSpecJson string) {
		mockedHTTPServer.AppendHandlers(
			ghttp.CombineHandlers(
				ghttp.VerifyRequest("DELETE", "/limits"),
				ghttp.VerifyJSON(limitSpecJson),
			),
		)
	}

	// These tests make HTTP requests to the same mocked server. Running them in
	// parallel makes it difficult to reason about them.
	var sequentialTestLock sync.Mutex

	BeforeEach(func() {
		sequentialTestLock.Lock()
		defer sequentialTestLock.Unlock()
		mockedHTTPServer.Reset()
	})

	Context("Creating a new RateLimit object", func() {
		testLimit := newRateLimit()
		testLimitSpecJson, _ := json.Marshal(testLimit.Spec)

		BeforeEach(func() {
			addHandlerForLimitCreation(string(testLimitSpecJson))
		})

		AfterEach(func() {
			Expect(runDeleteAndWaitHTTPReq(k8sClient.Delete)(
				context.TODO(), &testLimit,
			)).Should(Succeed())
		})

		It("Should create a limit in Limitador", func() {
			Expect(runCreateAndWaitHTTPReq(k8sClient.Create)(
				context.TODO(), &testLimit,
			)).Should(Succeed())
		})
	})

	Context("Deleting a RateLimit object", func() {
		testLimit := newRateLimit()
		testLimitSpecJson, _ := json.Marshal(testLimit.Spec)

		BeforeEach(func() {
			addHandlerForLimitCreation(string(testLimitSpecJson))

			Expect(runCreateAndWaitHTTPReq(k8sClient.Create)(
				context.TODO(), &testLimit,
			)).Should(Succeed())

			addHandlerForLimitDeletion(string(testLimitSpecJson))
		})

		It("Should delete the limit in Limitador", func() {
			Expect(runDeleteAndWaitHTTPReq(k8sClient.Delete)(
				context.TODO(), &testLimit,
			)).Should(Succeed())
		})
	})
})
