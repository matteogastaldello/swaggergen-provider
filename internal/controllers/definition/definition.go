package definition

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	fgetter "github.com/hashicorp/go-getter"

	"github.com/krateoplatformops/provider-runtime/pkg/controller"
	"github.com/krateoplatformops/provider-runtime/pkg/event"
	"github.com/krateoplatformops/provider-runtime/pkg/logging"
	"github.com/krateoplatformops/provider-runtime/pkg/ratelimiter"
	definitionv1alpha1 "github.com/matteogastaldello/swaggergen-provider/apis/definitions/v1alpha1"
	"github.com/pb33f/libopenapi"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/krateoplatformops/provider-runtime/pkg/reconciler"
	"github.com/krateoplatformops/provider-runtime/pkg/resource"

	"github.com/matteogastaldello/swaggergen-provider/internal/controllers/compositiondefinition/generator"
	"github.com/matteogastaldello/swaggergen-provider/internal/tools/crds"
	"github.com/matteogastaldello/swaggergen-provider/internal/tools/deployment"
	"github.com/matteogastaldello/swaggergen-provider/internal/tools/generation"

	"github.com/krateoplatformops/crdgen"
	// "github.com/matteogastaldello/swaggergen-provider/internal/crdgen"
	"github.com/matteogastaldello/swaggergen-provider/internal/tools/generator/text"
)

const (
	errNotDefinition = "managed resource is not a Definition"
	labelKeyGroup    = "krateo.io/crd-group"
	labelKeyVersion  = "krateo.io/crd-version"
	labelKeyResource = "krateo.io/crd-resource"
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
	var err error
	swaggerPath := cr.Spec.SwaggerPath

	basePath := "/tmp/swaggergen-provider"
	err = os.MkdirAll(basePath, os.ModePerm)
	defer os.RemoveAll(basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}
	const errorLocalPath = "relative paths require a module with a pwd"
	err = fgetter.GetFile(filepath.Join(basePath, filepath.Base(swaggerPath)), swaggerPath)
	if err != nil && err.Error() == errorLocalPath {
		swaggerPath, err = filepath.Abs(swaggerPath)
		if err != nil {
			return nil, fmt.Errorf("failed to get absolute path: %w", err)
		}
		err = fgetter.GetFile(filepath.Join(basePath, filepath.Base(swaggerPath)), swaggerPath)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to download file: %w", err)
	}

	contents, _ := os.ReadFile(path.Join(basePath, path.Base(swaggerPath)))
	d, err := libopenapi.NewDocument(contents)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	doc, modelErrors := d.BuildV3Model()
	if len(modelErrors) > 0 {
		return nil, fmt.Errorf("failed to build model: %w", errors.Join(modelErrors...))
	}
	if doc == nil {
		return nil, fmt.Errorf("failed to build model")
	}

	// Resolve model references
	resolvingErrors := doc.Index.GetResolver().Resolve()
	errs := []error{}
	for i := range resolvingErrors {
		c.log.Debug("Resolving error", "error", resolvingErrors[i].Error())
		errs = append(errs, resolvingErrors[i].ErrorRef)
	}
	if len(resolvingErrors) > 0 {
		return nil, fmt.Errorf("failed to resolve model references: %w", errors.Join(errs...))
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
	doc  *libopenapi.DocumentModel[v3.Document]
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

	err := generator.GenerateByteSchemas(e.doc, cr.Spec.Resource, cr.Spec.Identifier)
	if err != nil {
		return fmt.Errorf("generating byte schemas: %w", err)
	}

	resource := crdgen.Generate(ctx, crdgen.Options{
		Managed: true,
		WorkDir: fmt.Sprintf("gen-crds/%s", cr.Spec.Resource.Kind),
		GVK: schema.GroupVersionKind{
			Group:   cr.Spec.ResourceGroup,
			Version: "v1alpha1",
			Kind:    text.CapitaliseFirstLetter(cr.Spec.Resource.Kind),
		},
		Categories:             []string{strings.ToLower(cr.Spec.Resource.Kind)},
		SpecJsonSchemaGetter:   generator.OASSpecJsonSchemaGetter(e.doc, cr.Spec.Resource),
		StatusJsonSchemaGetter: generator.OASStatusJsonSchemaGetter(e.doc, cr.Spec.Identifier),
	})

	if resource.Err != nil {
		return fmt.Errorf("generating CRD: %w", resource.Err)
	}

	crd, err := crds.UnmarshalCRD(resource.Manifest)
	if err != nil {
		return fmt.Errorf("unmarshalling CRD: %w", err)
	}

	err = crds.InstallCRD(ctx, e.kube, crd)
	if err != nil {
		return fmt.Errorf("installing CRD: %w", err)
	}

	for secSchemaPair := e.doc.Model.Components.SecuritySchemes.First(); secSchemaPair != nil; secSchemaPair = secSchemaPair.Next() {
		if !generation.IsValidAuthSchema(secSchemaPair.Value()) {
			continue
		}
		resource = crdgen.Generate(ctx, crdgen.Options{
			Managed: false,
			WorkDir: fmt.Sprintf("gen-crds/%s", secSchemaPair.Key()),
			GVK: schema.GroupVersionKind{
				Group:   cr.Spec.ResourceGroup,
				Version: "v1alpha1",
				Kind:    fmt.Sprintf("%sAuth", text.CapitaliseFirstLetter(secSchemaPair.Key())),
			},
			Categories:             []string{strings.ToLower(cr.Spec.Resource.Kind)},
			SpecJsonSchemaGetter:   generator.OASAuthJsonSchemaGetter(secSchemaPair.Value(), cr.Spec.Resource),
			StatusJsonSchemaGetter: generator.OASStatusJsonSchemaGetter(e.doc, cr.Spec.Identifier),
		})

		if resource.Err != nil {
			return fmt.Errorf("generating CRD: %w", resource.Err)
		}

		crd, err := crds.UnmarshalCRD(resource.Manifest)
		if err != nil {
			return fmt.Errorf("unmarshalling CRD: %w", err)
		}

		err = crds.InstallCRD(ctx, e.kube, crd)
		if err != nil {
			return fmt.Errorf("installing CRD: %w", err)
		}
	}

	err = deployment.Deploy(ctx, deployment.DeployOptions{
		KubeClient: e.kube,
		NamespacedName: types.NamespacedName{
			Namespace: cr.Namespace,
			Name:      cr.Name,
		},
		Spec:            &cr.Spec,
		ResourceVersion: "v1alpha1",
	})
	if err != nil {
		return fmt.Errorf("deploying controller: %w", err)
	}

	cr.Status.Created = true
	err = e.kube.Status().Update(ctx, cr)

	e.log.Debug("Creating Definition", "Path:", cr.Spec.SwaggerPath, "Group:", cr.Spec.ResourceGroup)
	e.rec.Eventf(cr, corev1.EventTypeNormal, "DefinitionCreating",
		"Definition '%s/%s' creating", cr.Spec.SwaggerPath, cr.Spec.ResourceGroup)
	return err
}

func (e *external) Update(ctx context.Context, mg resource.Managed) error {
	return nil
}

func (e *external) Delete(ctx context.Context, mg resource.Managed) error {
	return nil
}
