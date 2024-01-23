package definition

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"k8s.io/client-go/tools/record"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/krateoplatformops/provider-runtime/pkg/controller"
	"github.com/krateoplatformops/provider-runtime/pkg/event"
	"github.com/krateoplatformops/provider-runtime/pkg/logging"
	"github.com/krateoplatformops/provider-runtime/pkg/ratelimiter"
	definitionv1alpha1 "github.com/matteogastaldello/swaggergen-provider/apis/definitions/v1alpha1"

	"github.com/krateoplatformops/provider-runtime/pkg/reconciler"
	"github.com/krateoplatformops/provider-runtime/pkg/resource"

	generation "github.com/matteogastaldello/swaggergen-provider/internal/tools/generation"
	"github.com/matteogastaldello/swaggergen-provider/internal/tools/generator/code"
	"github.com/matteogastaldello/swaggergen-provider/internal/tools/generator/text"
	swagger "github.com/matteogastaldello/swaggergen-provider/internal/tools/swagger"
)

const (
	errNotDefinition = "managed resource is not a Definition"
)

func Setup(mgr ctrl.Manager, o controller.Options) error {
	name := reconciler.ControllerName(definitionv1alpha1.DefinitionGroupKind)

	log := o.Logger.WithValues("controller", name)

	recorder := mgr.GetEventRecorderFor(name)

	r := reconciler.NewReconciler(mgr,
		resource.ManagedKind(definitionv1alpha1.DefinitionGroupVersionKind),
		reconciler.WithExternalConnecter(&connector{
			kube:     mgr.GetClient(),
			log:      log,
			recorder: recorder,
		}),
		reconciler.WithLogger(log),
		reconciler.WithRecorder(event.NewAPIRecorder(recorder)))

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o.ForControllerRuntime()).
		For(&definitionv1alpha1.Definition{}).
		Complete(ratelimiter.NewReconciler(name, r, o.GlobalRateLimiter))
}

type connector struct {
	kube     client.Client
	log      logging.Logger
	recorder record.EventRecorder
}

func (c *connector) Connect(ctx context.Context, mg resource.Managed) (reconciler.ExternalClient, error) {
	cr, ok := mg.(*definitionv1alpha1.Definition)
	if !ok {
		return nil, errors.New(errNotDefinition)
	}

	doc, err := swagger.LoadSchema(cr.Spec.SwaggerPath)
	if err != nil {
		return nil, err
	}

	return &external{
		kube: c.kube,
		log:  c.log,
		doc:  doc,
		rec:  c.recorder,
	}, nil
}

// An ExternalClient observes, then either creates, updates, or deletes an
// external resource to ensure it reflects the managed resource's desired state.
type external struct {
	kube client.Client
	log  logging.Logger
	doc  *openapi3.T
	rec  record.EventRecorder
}

func (e *external) Observe(ctx context.Context, mg resource.Managed) (reconciler.ExternalObservation, error) {
	cr, ok := mg.(*definitionv1alpha1.Definition)
	if !ok {
		return reconciler.ExternalObservation{}, errors.New(errNotDefinition)
	}

	if cr.Status.Created {
		return reconciler.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: true,
		}, nil
	}

	return reconciler.ExternalObservation{
		ResourceExists: false,
	}, nil
}

func (e *external) Create(ctx context.Context, mg resource.Managed) error {
	cr, ok := mg.(*definitionv1alpha1.Definition)
	if !ok {
		return errors.New(errNotDefinition)
	}

	resources := cr.Spec.Resources
	byteSchema, err := generation.GenerateJsonSchema(e.doc)
	if err != nil {
		return err
	}

	for _, resource := range resources {
		fmt.Println(string(byteSchema[resource.Kind]))
		err = code.Do(&code.Resource{
			Group:      cr.Spec.ResourceGroup,
			Version:    "v1alpha1",
			Kind:       text.CapitaliseFirstLetter(resource.Kind),
			Categories: []string{strings.ToLower(resource.Kind)},
			Schema:     byteSchema[resource.Kind],
			Identifier: resource.Identifier,
		},
			code.Options{
				Module:  "github.com/matteogastaldello/swaggergen-provider",
				Workdir: "./tmp/crd-gen",
			},
		)
		if err != nil {
			return err
		}
	}

	cr.Status.Created = true
	e.kube.Status().Update(ctx, cr)

	e.log.Debug("Creating Definition", "Path:", cr.Spec.SwaggerPath, "Group:", cr.Spec.ResourceGroup)
	e.rec.Eventf(cr, corev1.EventTypeNormal, "DefinitionCreating",
		"Definition '%s/%s' creating", cr.Spec.SwaggerPath, cr.Spec.ResourceGroup)
	return nil
}

func (e *external) Update(ctx context.Context, mg resource.Managed) error {
	return nil
}

func (e *external) Delete(ctx context.Context, mg resource.Managed) error {
	return nil
}
